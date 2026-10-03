// Package ops runs the user's read-only status commands and parses their pitwall.status/1 JSON.
// Pitwall holds no credentials: each command authenticates on its own and must not change anything.
package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/goriparthi/pitwall/internal/config"
	"github.com/goriparthi/pitwall/internal/logx"
)

const (
	Schema      = "pitwall.status/1"
	maxStdout   = 64 << 10
	maxItems    = 8
	maxFindings = 6
	maxCharts   = 3
	maxSeries   = 6
	maxPoints   = 400
	failingRuns = 3 // consecutive read failures before the widget says there is no reading
)

type Item struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Value  string `json:"value"`
	Detail string `json:"detail,omitempty"`
	Series string `json:"series,omitempty"` // a chart series name; the panel shows that series' color as the item's swatch
}

type Finding struct {
	Status string `json:"status"`
	Text   string `json:"text"`
}

// Series is one line, bar set or area: [epoch ms, value] points, oldest first.
type Series struct {
	Name   string       `json:"name"`
	Kind   string       `json:"kind,omitempty"`  // area | bar | line; set by the chart kind when empty
	Color  string       `json:"color,omitempty"` // #RRGGBB; empty means the panel's neutral for that kind
	Points [][2]float64 `json:"points"`
}

// Chart kinds: stacked-area (every series an area, stacked in order) and bars-line (bar and line series on one axis).
type Chart struct {
	Key    string   `json:"key"`
	Label  string   `json:"label"`
	Kind   string   `json:"kind"`
	Unit   string   `json:"unit,omitempty"`
	Series []Series `json:"series"`
}

// Reading is one parsed status document.
type Reading struct {
	Status   string    `json:"status"` // ok | warn | crit | unknown
	AsOf     int64     `json:"asOf"`
	Summary  string    `json:"summary"`
	Items    []Item    `json:"items"`
	Findings []Finding `json:"findings"`
	Charts   []Chart   `json:"charts"`
}

// Source is one configured command's latest good reading plus how its recent runs went.
type Source struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Reading
	HasReading bool   `json:"hasReading"`
	CheckedAt  int64  `json:"checkedAt,omitempty"`
	LastOkAt   int64  `json:"lastOkAt,omitempty"`
	Failures   int    `json:"failures"`
	Error      string `json:"error,omitempty"` // timeout | exit N | invalid json | not found | output too large
	Stale      bool   `json:"stale"`
	Failing    bool   `json:"failing"`
}

type Snapshot struct {
	Present bool     `json:"present"`
	Sources []Source `json:"sources"`
}

var (
	statuses    = map[string]bool{"ok": true, "warn": true, "crit": true, "unknown": true}
	chartKinds  = map[string]string{"stacked-area": "area", "bars-line": "bar"}
	seriesKinds = map[string]bool{"area": true, "bar": true, "line": true}
	colorRe     = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
)

// charts keeps well-formed charts only; a malformed chart is dropped rather than failing the whole reading.
func charts(raw []Chart) []Chart {
	out := []Chart{}
	for _, c := range raw {
		if len(out) == maxCharts {
			break
		}
		def, ok := chartKinds[c.Kind]
		if !ok || len(c.Series) == 0 {
			continue
		}
		ch := Chart{Key: clip(c.Key, 40), Label: clip(c.Label, 40), Kind: c.Kind, Unit: clip(c.Unit, 16), Series: []Series{}}
		for _, sr := range c.Series {
			if len(ch.Series) == maxSeries {
				break
			}
			kind := sr.Kind
			if kind == "" || c.Kind == "stacked-area" {
				kind = def
			}
			if !seriesKinds[kind] {
				continue
			}
			color := ""
			if colorRe.MatchString(sr.Color) {
				color = strings.ToLower(sr.Color)
			}
			pts := sr.Points
			if len(pts) > maxPoints {
				pts = pts[len(pts)-maxPoints:]
			}
			if pts == nil {
				pts = [][2]float64{}
			}
			ch.Series = append(ch.Series, Series{Name: clip(sr.Name, 24), Kind: kind, Color: color, Points: pts})
		}
		out = append(out, ch)
	}
	return out
}

func clip(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, strings.TrimSpace(s))
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

func normStatus(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if statuses[s] {
		return s
	}
	return "unknown"
}

var errInvalid = errors.New("invalid json")

// Parse reads a pitwall.status/1 document. Exit codes 0, 1 and 2 mean ok, warn and crit when the
// document has no status; any other exit code is a failed read, not a pipeline status.
func Parse(stdout []byte, exitCode int, now time.Time) (Reading, error) {
	if exitCode < 0 || exitCode > 2 {
		return Reading{}, fmt.Errorf("exit %d", exitCode)
	}
	var raw struct {
		Schema   string    `json:"schema"`
		Status   string    `json:"status"`
		AsOf     string    `json:"asOf"`
		Summary  string    `json:"summary"`
		Items    []Item    `json:"items"`
		Findings []Finding `json:"findings"`
		Charts   []Chart   `json:"charts"`
	}
	if err := json.Unmarshal(stdout, &raw); err != nil || raw.Schema != Schema {
		return Reading{}, errInvalid
	}
	r := Reading{Status: [...]string{"ok", "warn", "crit"}[exitCode], AsOf: now.UnixMilli(), Summary: clip(raw.Summary, 80)}
	if raw.Status != "" {
		r.Status = normStatus(raw.Status)
	}
	if raw.AsOf != "" {
		t, err := time.Parse(time.RFC3339, raw.AsOf)
		if err != nil {
			return Reading{}, errInvalid
		}
		r.AsOf = t.UnixMilli()
	}
	r.Items = []Item{}
	for _, it := range raw.Items {
		if len(r.Items) == maxItems {
			break
		}
		r.Items = append(r.Items, Item{Key: clip(it.Key, 40), Label: clip(it.Label, 32), Status: normStatus(it.Status), Value: clip(it.Value, 24), Detail: clip(it.Detail, 48), Series: clip(it.Series, 24)})
	}
	r.Charts = charts(raw.Charts)
	r.Findings = []Finding{}
	for _, f := range raw.Findings {
		if len(r.Findings) == maxFindings {
			break
		}
		r.Findings = append(r.Findings, Finding{Status: normStatus(f.Status), Text: clip(f.Text, 120)})
	}
	return r, nil
}

// capped keeps the first n bytes and records whether more arrived.
type capped struct {
	buf  []byte
	n    int
	over bool
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.n - len(c.buf); room > 0 {
		c.buf = append(c.buf, p[:min(room, len(p))]...)
	}
	if len(c.buf)+len(p) > c.n {
		c.over = true
	}
	return len(p), nil
}

// run executes one source and returns its reading, or a short error class plus stderr for the log.
func run(ctx context.Context, src config.OpsSource) (Reading, string, string) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(src.TimeoutSeconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, src.Command[0], src.Command[1:]...)
	cmd.Dir, _ = os.UserHomeDir()
	cmd.Env = append(os.Environ(), "NO_COLOR=1")
	killGroup(cmd)
	cmd.WaitDelay = 2 * time.Second
	out, errOut := &capped{n: maxStdout}, &capped{n: 200}
	cmd.Stdout, cmd.Stderr = out, errOut
	err := cmd.Run()
	stderr := strings.TrimSpace(string(errOut.buf))
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		return Reading{}, "timeout", stderr
	case errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist):
		return Reading{}, "not found", stderr
	case out.over:
		return Reading{}, "output too large", stderr
	}
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		return Reading{}, "not found", err.Error()
	}
	r, perr := Parse(out.buf, code, time.Now())
	if perr != nil {
		return Reading{}, perr.Error(), stderr
	}
	return r, "", stderr
}

type state struct {
	src     Source
	next    time.Time
	running bool
	conf    string // the source's command and timing; a change runs it again at once
}

func confKey(src config.OpsSource) string {
	return fmt.Sprintf("%q %d %d", src.Command, src.IntervalSeconds, src.TimeoutSeconds)
}

// Collector polls every configured source on its own interval and follows config reloads.
type Collector struct {
	Cfg func() *config.Config
	Log *logx.Logger

	mu    sync.Mutex
	state map[string]*state
}

func (c *Collector) Start(ctx context.Context) {
	c.state = map[string]*state{}
	c.tick(ctx, time.Now())
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				c.tick(ctx, now)
			}
		}
	}()
}

// tick drops sources removed from the config and starts any that are due and not already running.
func (c *Collector) tick(ctx context.Context, now time.Time) {
	srcs := c.Cfg().Ops.Sources
	c.mu.Lock()
	defer c.mu.Unlock()
	keep := map[string]bool{}
	for _, src := range srcs {
		keep[src.ID] = true
		st := c.state[src.ID]
		if st == nil {
			st = &state{src: Source{ID: src.ID}}
			c.state[src.ID] = st
		}
		st.src.Label = src.Label
		if k := confKey(src); k != st.conf {
			st.conf, st.next = k, time.Time{}
		}
		if st.running || now.Before(st.next) {
			continue
		}
		st.running = true
		st.next = now.Add(time.Duration(src.IntervalSeconds) * time.Second)
		go c.read(ctx, src)
	}
	for id := range c.state {
		if !keep[id] {
			delete(c.state, id)
		}
	}
}

func (c *Collector) read(ctx context.Context, src config.OpsSource) {
	r, errClass, stderr := run(ctx, src)
	if ctx.Err() != nil {
		return
	}
	now := time.Now().UnixMilli()
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.state[src.ID]
	if st == nil {
		return // removed from the config while it ran
	}
	st.running = false
	st.src.CheckedAt = now
	if errClass != "" {
		st.src.Failures++
		st.src.Error = errClass
		if c.Log != nil {
			c.Log.Warn("ops read failed", map[string]any{"source": src.ID, "error": errClass, "stderr": stderr})
		}
		return
	}
	st.src.Reading, st.src.HasReading, st.src.LastOkAt, st.src.Failures, st.src.Error = r, true, now, 0, ""
}

// Snapshot lists sources in config order with staleness worked out, so the views stay simple.
func (c *Collector) Snapshot(now time.Time) Snapshot {
	srcs := c.Cfg().Ops.Sources
	out := Snapshot{Present: len(srcs) > 0, Sources: []Source{}}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, cfgSrc := range srcs {
		s := Source{ID: cfgSrc.ID, Label: cfgSrc.Label, Reading: Reading{Status: "unknown", Items: []Item{}, Findings: []Finding{}, Charts: []Chart{}}}
		if st := c.state[cfgSrc.ID]; st != nil {
			s = st.src
			s.Label = cfgSrc.Label
			if !s.HasReading {
				s.Reading = Reading{Status: "unknown", Items: []Item{}, Findings: []Finding{}, Charts: []Chart{}}
			}
		}
		limit := 2*time.Duration(cfgSrc.IntervalSeconds)*time.Second + time.Duration(cfgSrc.TimeoutSeconds)*time.Second
		s.Stale = s.HasReading && now.Sub(time.UnixMilli(s.AsOf)) > limit
		s.Failing = s.Failures >= failingRuns
		out.Sources = append(out.Sources, s)
	}
	return out
}

// Critical reports whether any source has a fresh, trusted critical reading; old or failing data never lights the LED.
func (s Snapshot) Critical() bool {
	for _, src := range s.Sources {
		if src.HasReading && src.Status == "crit" && !src.Stale && !src.Failing {
			return true
		}
	}
	return false
}
