package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/process"

	"github.com/goriparthi/pitwall/internal/config"
	"github.com/goriparthi/pitwall/internal/logx"
)

const feedMax = 60

var slugRe = regexp.MustCompile(`[^A-Za-z0-9]`)

// ProjectSlug matches Claude Code's ~/.claude/projects/<slug> naming.
func ProjectSlug(cwd string) string { return slugRe.ReplaceAllString(cwd, "-") }

func ClaudeDir() string {
	if d := os.Getenv("PITWALL_CLAUDE_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

type feedEntry struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	At        int64  `json:"at"`
	FeedItem
}

type Collector struct {
	log       *logx.Logger
	holdMs    func() int64
	mu        sync.Mutex
	hooks     map[string]*Session
	registry  map[string]*Registry
	tails     map[string]*Tail
	ledger    map[string]*Tail
	feed      []feedEntry
	acked     map[string]bool
	lastHook  int64
	eventsLog string
}

func New(log *logx.Logger, holdMinutes func() int) *Collector {
	c := &Collector{
		log:       log,
		holdMs:    func() int64 { return int64(holdMinutes()) * 60000 },
		hooks:     map[string]*Session{},
		registry:  map[string]*Registry{},
		tails:     map[string]*Tail{},
		ledger:    map[string]*Tail{},
		acked:     map[string]bool{},
		eventsLog: filepath.Join(config.StateDir(), "claude-events.jsonl"),
	}
	c.restore()
	return c
}

// restore replays today's sanitized events so a restart keeps the feed and hook state.
func (c *Collector) restore() {
	f, err := os.Open(c.eventsLog)
	if err != nil {
		return
	}
	since := StartOfToday()
	var keep []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var e Event
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.At < since || e.SessionID == "" {
			continue
		}
		keep = append(keep, sc.Text())
		c.apply(e)
	}
	f.Close()
	data := strings.Join(keep, "\n")
	if data != "" {
		data += "\n"
	}
	_ = os.WriteFile(c.eventsLog, []byte(data), 0o600)
}

func (c *Collector) apply(e Event) {
	s := c.hooks[e.SessionID]
	if s == nil {
		s = &Session{}
		c.hooks[e.SessionID] = s
	}
	if item := Apply(s, e); item != nil {
		c.feed = append([]feedEntry{{ID: fmt.Sprintf("%s:%d:%s", e.SessionID, e.At, item.Kind), SessionID: e.SessionID, At: e.At, FeedItem: *item}}, c.feed...)
		if len(c.feed) > feedMax {
			c.feed = c.feed[:feedMax]
		}
	}
	c.lastHook = e.At
}

// Ingest takes a raw hook payload; only the sanitized form is kept and logged.
func (c *Collector) Ingest(raw map[string]any) bool {
	e := Sanitize(raw)
	if e.SessionID == "" || e.Event == "" {
		return false
	}
	c.mu.Lock()
	c.apply(e)
	c.mu.Unlock()
	if b, err := json.Marshal(e); err == nil {
		if f, err := os.OpenFile(c.eventsLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			_, _ = f.Write(append(b, '\n'))
			f.Close()
		}
	}
	return true
}

func (c *Collector) Ack(id string) {
	c.mu.Lock()
	c.acked[id] = true
	c.mu.Unlock()
}

func (c *Collector) Start(ctx context.Context) {
	loop := func(every time.Duration, name string, fn func()) {
		go func() {
			t := time.NewTicker(every)
			defer t.Stop()
			for {
				func() {
					defer func() {
						if r := recover(); r != nil {
							c.log.Error("claude collector panic", map[string]any{"task": name, "panic": fmt.Sprint(r)})
						}
					}()
					fn()
				}()
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}
			}
		}()
	}
	c.scanRegistry() // so the first transcript pass already knows the live sessions
	loop(1500*time.Millisecond, "registry", c.scanRegistry)
	loop(3*time.Second, "transcripts", c.scanTranscripts)
	loop(60*time.Second, "ledger", c.scanLedger)
}

func (c *Collector) scanRegistry() {
	dir := filepath.Join(ClaudeDir(), "sessions")
	entries, _ := os.ReadDir(dir)
	next := map[string]*Registry{}
	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, ".json") || strings.Trim(strings.TrimSuffix(n, ".json"), "0123456789") != "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		var r Registry
		if json.Unmarshal(data, &r) != nil || r.SessionID == "" {
			continue // being rewritten; next scan picks it up
		}
		if ok, _ := process.PidExists(int32(r.PID)); !ok {
			continue
		}
		if r.StatusUpdatedAt == 0 {
			r.StatusUpdatedAt = r.UpdatedAt
		}
		next[r.SessionID] = &r
	}
	c.mu.Lock()
	c.registry = next
	c.mu.Unlock()
}

func (c *Collector) scanTranscripts() {
	c.mu.Lock()
	want := map[string]string{}
	for id, r := range c.registry {
		want[id] = filepath.Join(ClaudeDir(), "projects", ProjectSlug(r.Cwd), id+".jsonl")
	}
	for id, s := range c.hooks {
		if s.Phase == "ended" {
			continue
		}
		if s.TranscriptPath != "" {
			want[id] = s.TranscriptPath
		} else if _, ok := want[id]; !ok && s.Cwd != "" {
			want[id] = filepath.Join(ClaudeDir(), "projects", ProjectSlug(s.Cwd), id+".jsonl")
		}
	}
	tails := map[string]*Tail{}
	for id, p := range want {
		t := c.tails[id]
		if t == nil || t.Path != p {
			t = NewTail(p)
		}
		tails[id] = t
	}
	c.tails = tails
	c.mu.Unlock()
	for _, t := range tails {
		t.Read() // file I/O outside the lock; Tail is only touched by this loop
	}
}

// scanLedger totals today's usage across every transcript touched today, ended sessions included.
func (c *Collector) scanLedger() {
	since := StartOfToday()
	root := filepath.Join(ClaudeDir(), "projects")
	dirs, _ := os.ReadDir(root)
	next := map[string]*Tail{}
	c.mu.Lock()
	old := c.ledger
	c.mu.Unlock()
	for _, d := range dirs {
		files, _ := os.ReadDir(filepath.Join(root, d.Name()))
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			info, err := f.Info()
			if err != nil || info.ModTime().UnixMilli() < since {
				continue
			}
			p := filepath.Join(root, d.Name(), f.Name())
			t := old[p]
			if t != nil && t.Day != since {
				t = nil
			}
			if t == nil {
				t = NewTail(p)
			}
			t.Read()
			next[p] = t
		}
	}
	c.mu.Lock()
	c.ledger = next
	c.mu.Unlock()
}

type Agent struct {
	ID           string `json:"id"`
	Tool         string `json:"tool"`
	Label        string `json:"label"`
	Name         string `json:"name,omitempty"`
	Project      string `json:"project,omitempty"`
	Cwd          string `json:"cwd,omitempty"`
	Status       string `json:"status"`
	StatusSource string `json:"statusSource"`
	Since        int64  `json:"since,omitempty"`
	StartedAt    int64  `json:"startedAt,omitempty"`
	Task         string `json:"task,omitempty"`
	LastEvent    string `json:"lastEvent,omitempty"`
	Wait         *Wait  `json:"wait,omitempty"`
	Error        string `json:"error,omitempty"`
	LastTurnMs   int64  `json:"lastTurnMs,omitempty"`
	Subagents    int    `json:"subagents"`
	Hooks        bool   `json:"hooks"`
	Usage        *Usage `json:"usage"`
}

type Attention struct {
	ID      string `json:"id"`
	AgentID string `json:"agentId"`
	Kind    string `json:"kind"`
	Since   int64  `json:"since"`
	Name    string `json:"name,omitempty"`
	Project string `json:"project,omitempty"`
	Detail  any    `json:"detail,omitempty"`
}

type Snapshot struct {
	Agents      []Agent        `json:"agents"`
	Attention   []Attention    `json:"attention"`
	Feed        []feedEntry    `json:"feed"`
	Today       map[string]any `json:"today"`
	Integration map[string]any `json:"integration"`
}

var statusOrder = map[string]int{"waiting": 0, "failed": 1, "working": 2, "completed": 3, "idle": 4, "unknown": 5}

func (c *Collector) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().UnixMilli()
	ids := map[string]bool{}
	for id := range c.registry {
		ids[id] = true
	}
	for id, s := range c.hooks {
		// hook-only sessions (e.g. non-interactive runs) stay visible while recent
		if s.Phase != "ended" && now-s.LastHookAt < 30*60000 {
			ids[id] = true
		}
	}
	agents := []Agent{}
	for id := range ids {
		r, h := c.registry[id], c.hooks[id]
		if r == nil && h != nil && h.Phase == "ended" {
			continue
		}
		status, source := Fuse(h, r, now, c.holdMs())
		a := Agent{ID: id, Tool: "claude-code", Label: "Claude Code", Status: status, StatusSource: source}
		if r != nil {
			a.Name, a.Cwd, a.StartedAt = r.Name, r.Cwd, r.StartedAt
		}
		if h != nil {
			if a.Cwd == "" {
				a.Cwd = h.Cwd
			}
			if a.StartedAt == 0 {
				a.StartedAt = h.StartedAt
			}
			a.LastEvent, a.LastTurnMs, a.Subagents, a.Hooks = h.LastEvent, h.LastTurnMs, h.Subagents, h.LastHookAt > 0
		}
		if a.Cwd != "" {
			a.Project = filepath.Base(a.Cwd)
		}
		if t := c.tails[id]; t != nil {
			if u, found := t.Snap(); found {
				a.Task, a.Usage = u.Title, &u
			}
		}
		switch status {
		case "working":
			if h != nil && h.TurnStartedAt > 0 && source == "hooks" {
				a.Since = h.TurnStartedAt
			} else if r != nil {
				a.Since = r.StatusUpdatedAt
			}
		case "waiting":
			a.Wait = &Wait{Kind: "approval"}
			if h != nil && h.Wait != nil {
				a.Wait = h.Wait
				a.Since = h.Wait.Since
			}
		case "completed":
			a.Since = h.CompletedAt
		case "failed":
			a.Since, a.Error = h.FailedAt, h.Error
		default:
			if r != nil {
				a.Since = r.StatusUpdatedAt
			} else if h != nil {
				a.Since = h.LastHookAt
			}
		}
		agents = append(agents, a)
	}
	sort.Slice(agents, func(i, j int) bool {
		if statusOrder[agents[i].Status] != statusOrder[agents[j].Status] {
			return statusOrder[agents[i].Status] < statusOrder[agents[j].Status]
		}
		return agents[i].Since > agents[j].Since
	})

	attention := []Attention{}
	for _, a := range agents {
		if a.Status != "waiting" && a.Status != "failed" && a.Status != "completed" {
			continue
		}
		x := Attention{ID: fmt.Sprintf("%s:%s:%d", a.ID, a.Status, a.Since), AgentID: a.ID, Kind: a.Status, Since: a.Since, Name: a.Name, Project: a.Project}
		switch a.Status {
		case "waiting":
			x.Detail = a.Wait.Kind
		case "failed":
			x.Detail = a.Error
		default:
			x.Detail = a.LastTurnMs
		}
		if !c.acked[x.ID] {
			attention = append(attention, x)
		}
	}

	var in, out int64
	turns, sessions := 0, 0
	for _, t := range c.ledger {
		u, _ := t.Snap()
		if u.TodayInput > 0 || u.TodayOutput > 0 {
			in, out, turns, sessions = in+u.TodayInput, out+u.TodayOutput, turns+u.TodayTurns, sessions+1
		}
	}
	since := StartOfToday()
	done := 0
	for _, f := range c.feed {
		if f.Kind == "done" && f.At >= since {
			done++
		}
	}
	feed := c.feed
	if len(feed) > 20 {
		feed = feed[:20]
	}
	_, regErr := os.Stat(filepath.Join(ClaudeDir(), "sessions"))
	var lastHook any
	if c.lastHook > 0 {
		lastHook = c.lastHook
	}
	return Snapshot{
		Agents:      agents,
		Attention:   attention,
		Feed:        append([]feedEntry{}, feed...),
		Today:       map[string]any{"input": in, "output": out, "turns": turns, "sessions": sessions, "hookCompletions": done},
		Integration: map[string]any{"registry": regErr == nil, "hooksSeen": c.lastHook > 0, "lastHookEventAt": lastHook},
	}
}
