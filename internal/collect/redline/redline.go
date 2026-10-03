// Package redline reads Claude plan limits (5 hour session, week), pace, and usage totals from RedLine
// (github.com/goriparthi/redline). Order of preference: `redline status --json` (adds measured pace),
// RedLine's published usage-snapshot.json, then the statusline feed sidecar. All are local reads.
package redline

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

type Pace struct {
	RatePerHour     float64 `json:"ratePerHour"`
	ExhaustsAt      int64   `json:"exhaustsAt,omitempty"`
	HitsBeforeReset bool    `json:"hitsBeforeReset"`
	Basis           string  `json:"basis,omitempty"`
}

type Window struct {
	Key        string  `json:"key"`
	Name       string  `json:"name"`
	Provider   string  `json:"provider"`
	Used       float64 `json:"used"`
	ResetsAt   int64   `json:"resetsAt"`
	LengthMs   int64   `json:"lengthMs"`
	Provenance string  `json:"provenance,omitempty"`
	Pace       *Pace   `json:"pace,omitempty"`
}

type Totals struct {
	Tokens      float64 `json:"tokens"`
	CostUSD     float64 `json:"costUsd"`
	CostBasis   string  `json:"costBasis,omitempty"`
	TokensBasis string  `json:"tokensBasis,omitempty"`
	CostPartial bool    `json:"costPartial,omitempty"`
}

type Snapshot struct {
	Installed bool     `json:"installed"` // RedLine found on this machine; everything RedLine-related is hidden otherwise
	Available bool     `json:"available"`
	Reason    string   `json:"reason,omitempty"`
	Source    string   `json:"source,omitempty"`
	AsOf      int64    `json:"asOf,omitempty"`
	Windows   []Window `json:"windows"`
	Today     *Totals  `json:"today,omitempty"`
	Week      *Totals  `json:"week,omitempty"`
	CheckedAt int64    `json:"checkedAt"`
}

var windowLength = map[string]time.Duration{"five_hour": 5 * time.Hour, "seven_day": 7 * 24 * time.Hour}
var windowName = map[string]string{"five_hour": "Session", "seven_day": "Week"}

func ms(t string) int64 {
	if p, err := time.Parse(time.RFC3339Nano, t); err == nil {
		return p.UnixMilli()
	}
	return 0
}

func normalize(w Window) Window {
	if w.LengthMs == 0 {
		w.LengthMs = windowLength[w.Key].Milliseconds()
	}
	if w.Name == "" {
		w.Name = windowName[w.Key]
		if w.Name == "" {
			w.Name = w.Key
		}
	}
	if w.Provider == "" {
		w.Provider = "Claude"
	}
	return w
}

type cliTotals struct {
	CostBasis   string  `json:"cost_basis"`
	CostPartial bool    `json:"cost_partial"`
	CostUSD     float64 `json:"cost_usd"`
	Tokens      float64 `json:"tokens"`
	TokensBasis string  `json:"tokens_basis"`
}

func (t *cliTotals) convert() *Totals {
	if t == nil {
		return nil
	}
	return &Totals{Tokens: t.Tokens, CostUSD: t.CostUSD, CostBasis: t.CostBasis, TokensBasis: t.TokensBasis, CostPartial: t.CostPartial}
}

// ParseStatus reads `redline status --json`.
func ParseStatus(data []byte) (Snapshot, error) {
	var v struct {
		AsOf    string     `json:"claude_limits_as_of"`
		Updated string     `json:"updated_at"`
		Today   *cliTotals `json:"today"`
		Week    *cliTotals `json:"week"`
		Windows []struct {
			Key        string  `json:"key"`
			Display    string  `json:"display_name"`
			Provider   string  `json:"provider"`
			ResetsAt   string  `json:"resets_at"`
			Util       float64 `json:"utilization"`
			Provenance string  `json:"provenance"`
			Pace       *struct {
				Basis      string  `json:"basis"`
				ExhaustsAt string  `json:"exhausts_at"`
				Hits       bool    `json:"hits_limit_before_reset"`
				Rate       float64 `json:"rate_per_hour"`
			} `json:"pace"`
		} `json:"windows"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return Snapshot{}, err
	}
	s := Snapshot{Available: true, Source: "redline status", AsOf: ms(v.AsOf), Today: v.Today.convert(), Week: v.Week.convert()}
	if s.AsOf == 0 {
		s.AsOf = ms(v.Updated)
	}
	for _, w := range v.Windows {
		out := Window{Key: w.Key, Name: windowName[w.Key], Provider: w.Provider, Used: w.Util, ResetsAt: ms(w.ResetsAt), Provenance: w.Provenance}
		if w.Pace != nil {
			out.Pace = &Pace{RatePerHour: w.Pace.Rate, ExhaustsAt: ms(w.Pace.ExhaustsAt), HitsBeforeReset: w.Pace.Hits, Basis: w.Pace.Basis}
		}
		s.Windows = append(s.Windows, normalize(out))
	}
	return s, nil
}

// ParseUsageSnapshot reads RedLine's published usage-snapshot.json (standard keys plus a redline object).
func ParseUsageSnapshot(data []byte) (Snapshot, error) {
	var v struct {
		Updated string `json:"updated_at"`
		Redline *struct {
			AsOf    string     `json:"claude_limits_as_of"`
			Today   *cliTotals `json:"today"`
			Week    *cliTotals `json:"week"`
			Windows []struct {
				Key      string  `json:"key"`
				Provider string  `json:"provider"`
				ResetsAt string  `json:"resets_at"`
				Util     float64 `json:"utilization"`
				Source   string  `json:"source"`
			} `json:"windows"`
		} `json:"redline"`
		FiveHour *feedWindow `json:"five_hour"`
		SevenDay *feedWindow `json:"seven_day"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return Snapshot{}, err
	}
	s := Snapshot{Available: true, Source: "redline snapshot", AsOf: ms(v.Updated)}
	if v.Redline != nil {
		if a := ms(v.Redline.AsOf); a > 0 {
			s.AsOf = a
		}
		s.Today, s.Week = v.Redline.Today.convert(), v.Redline.Week.convert()
		for _, w := range v.Redline.Windows {
			s.Windows = append(s.Windows, normalize(Window{Key: w.Key, Provider: w.Provider, Used: w.Util, ResetsAt: ms(w.ResetsAt), Provenance: w.Source}))
		}
	}
	if len(s.Windows) == 0 {
		s.Windows = feedWindows(v.FiveHour, v.SevenDay)
	}
	return s, nil
}

type feedWindow struct {
	Used     *float64        `json:"used_percentage"`
	Util     *float64        `json:"utilization"`
	ResetsAt json.RawMessage `json:"resets_at"`
}

func (f *feedWindow) window(key string) (Window, bool) {
	if f == nil {
		return Window{}, false
	}
	used := f.Used
	if used == nil {
		used = f.Util
	}
	if used == nil {
		return Window{}, false
	}
	var reset int64
	var n float64
	var str string
	if json.Unmarshal(f.ResetsAt, &n) == nil {
		reset = int64(n) * 1000 // epoch seconds in the statusline payload
	} else if json.Unmarshal(f.ResetsAt, &str) == nil {
		reset = ms(str)
	}
	return normalize(Window{Key: key, Used: *used, ResetsAt: reset, Provenance: "statusline"}), true
}

func feedWindows(five, week *feedWindow) []Window {
	var out []Window
	if w, ok := five.window("five_hour"); ok {
		out = append(out, w)
	}
	if w, ok := week.window("seven_day"); ok {
		out = append(out, w)
	}
	return out
}

// ParseFeed reads the statusline sidecar (claude-usage.json): rate-limit windows only.
func ParseFeed(data []byte) (Snapshot, error) {
	var v struct {
		Updated  string      `json:"updated_at"`
		FiveHour *feedWindow `json:"five_hour"`
		SevenDay *feedWindow `json:"seven_day"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Available: true, Source: "statusline feed", AsOf: ms(v.Updated), Windows: feedWindows(v.FiveHour, v.SevenDay)}, nil
}

func dataDir() string {
	if d := os.Getenv("PITWALL_REDLINE_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "redline")
}

func findCLI() string {
	if p, set := os.LookupEnv("PITWALL_REDLINE_CLI"); set { // an explicit path is authoritative
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
		return ""
	}
	if p, err := exec.LookPath("redline"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{"/Applications/Redline.app/Contents/MacOS/redline", filepath.Join(home, "Applications/Redline.app/Contents/MacOS/redline")} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// readCLI runs the read-only status command. Exit codes 10 and 11 mean near or at a limit and still
// carry JSON; 20 and 30 mean nothing to report.
func readCLI(cli string) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, cli, "status", "--json").Output()
	var exit *exec.ExitError
	if err != nil && !(errors.As(err, &exit) && (exit.ExitCode() == 10 || exit.ExitCode() == 11)) {
		return Snapshot{}, err
	}
	return ParseStatus(out)
}

type Collector struct {
	mu   sync.RWMutex
	snap Snapshot
}

// Start reads once synchronously, so templates that need RedLine are known before the saved UI state
// is validated, then refreshes every minute.
func (c *Collector) Start(ctx context.Context) {
	c.read()
	go func() {
		t := time.NewTicker(60 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				c.read()
			}
		}
	}()
}

// Detect reports whether RedLine is set up here: its CLI, or the files it publishes for other tools.
func Detect() bool {
	if findCLI() != "" {
		return true
	}
	for _, f := range []string{"usage-snapshot.json", "claude-usage.json"} {
		if _, err := os.Stat(filepath.Join(dataDir(), f)); err == nil {
			return true
		}
	}
	return false
}

func (c *Collector) read() {
	s := Snapshot{Installed: Detect(), Reason: "no reading from RedLine yet"}
	if !s.Installed {
		s.Reason = "RedLine not installed"
		if runtime.GOOS == "windows" {
			s.Reason = "RedLine not installed (it runs on macOS today)"
		}
	}
	if cli := findCLI(); s.Installed && cli != "" {
		if got, err := readCLI(cli); err == nil && len(got.Windows) > 0 {
			got.Installed = true
			s = got
		}
	}
	if s.Installed && !s.Available {
		if data, err := os.ReadFile(filepath.Join(dataDir(), "usage-snapshot.json")); err == nil {
			if got, err := ParseUsageSnapshot(data); err == nil && len(got.Windows) > 0 {
				got.Installed = true
			s = got
			}
		}
	}
	if s.Installed && !s.Available {
		if data, err := os.ReadFile(filepath.Join(dataDir(), "claude-usage.json")); err == nil {
			if got, err := ParseFeed(data); err == nil && len(got.Windows) > 0 {
				got.Installed = true
			s = got
			}
		}
	}
	s.CheckedAt = time.Now().UnixMilli()
	if s.Windows == nil {
		s.Windows = []Window{}
	}
	c.mu.Lock()
	c.snap = s
	c.mu.Unlock()
}

func (c *Collector) Snapshot() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snap
}
