// Package demo produces synthetic, clearly labelled data for previewing the design.
package demo

import (
	"fmt"
	"math"
	"sort"
	"math/rand/v2"
	"sync"
	"time"
)

type M = map[string]any

type Source struct {
	mu      sync.Mutex
	t0      time.Time
	history map[string][]float64
}

func wave(t, period, lo, hi, phase float64) float64 {
	return lo + (hi-lo)*(1+math.Sin(t/period*2*math.Pi+phase))/2
}

func New() *Source {
	s := &Source{t0: time.Now(), history: map[string][]float64{"cpu": {}, "mem": {}, "netRx": {}, "netTx": {}, "gpu": {}}}
	for i := 0; i < 90; i++ {
		s.tick()
	}
	go func() {
		for range time.Tick(time.Second) {
			s.tick()
		}
	}()
	return s
}

func (s *Source) tick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := float64(time.Since(s.t0).Milliseconds())
	push := func(k string, v float64) {
		h := append(s.history[k], v)
		if len(h) > 90 {
			h = h[1:]
		}
		s.history[k] = h
	}
	push("cpu", wave(t, 23000, 12, 64, 0)+rand.Float64()*8)
	push("mem", wave(t, 90000, 58, 66, 0))
	push("netRx", wave(t, 11000, 2e5, 4.2e6, 0)*rand.Float64())
	push("netTx", wave(t, 17000, 5e4, 8e5, 0)*rand.Float64())
	push("gpu", wave(t, 31000, 4, 48, 0))
}

const storyMs = 36000

// story plays a 36 s loop: three agents work, api-gateway-3 asks to approve Bash at 8 s (until 20 s),
// and infra-7 finishes at 14 s. Elapsed times and token counts keep moving so the panel looks alive.
func (s *Source) story(n int64) M {
	elapsed := n - s.t0.UnixMilli()
	cycle := n - elapsed%storyMs // start of the current loop
	t := elapsed % storyMs
	u := func(model string, ctx, in, out float64, turns int) M {
		return M{"model": model, "contextTokens": ctx, "inputTokens": in, "outputTokens": out, "turns": turns}
	}
	grow := float64(t) / 1000
	agent := func(id, name, project, status string, since int64, task, ev string, extra M) M {
		a := M{"id": id, "tool": "claude-code", "label": "Claude Code", "name": name, "project": project, "status": status,
			"statusSource": "hooks", "since": since, "task": task, "lastEvent": ev, "hooks": true, "subagents": 0}
		for k, v := range extra {
			a[k] = v
		}
		return a
	}

	api := agent("d1", "api-gateway-3", "api-gateway", "working", cycle-290000, "Migrate rate limiter to sliding window", "Bash · go test ./...",
		M{"usage": u("claude-opus-5-5", 84000+grow*120, 2.1e6+grow*9000, 39800+grow*40, 6)})
	if t >= 8000 && t < 20000 {
		api["status"], api["since"] = "waiting", cycle+8000
		api["wait"] = M{"kind": "approval", "tool": "Bash", "since": cycle + 8000}
	} else if t >= 20000 {
		api["lastEvent"] = "Edit limiter.go"
	}
	webEvent := "Edit theme.css"
	if t >= 12000 {
		webEvent = "Bash · npm run build"
	}
	web := agent("d2", "web-dashboard-1", "web-dashboard", "working", cycle-312000, "Add dark mode tokens to settings page", webEvent,
		M{"subagents": 2, "usage": u("claude-sonnet-5-5", 58000+grow*150, 1.3e6+grow*12000, 27100+grow*55, 4)})
	infra := agent("d3", "infra-7", "infra", "working", cycle-520000, "Tighten S3 bucket policies", "Read main.tf",
		M{"usage": u("claude-opus-5-5", 118000+grow*60, 3.4e6, 64900+grow*30, 10)})
	if t >= 14000 {
		infra["status"], infra["since"], infra["lastTurnMs"] = "completed", cycle+14000, int64(534000)
		infra["usage"] = u("claude-opus-5-5", 120300, 3.4e6, 66100, 11)
	}
	notes := agent("d4", "notes-2", "notes", "idle", cycle-3600000, "Weekly summary draft", "", M{"usage": nil, "hooks": false, "statusSource": "registry"})

	// same ordering as the live collector: waiting, failed, working, completed, idle
	rank := map[string]int{"waiting": 0, "failed": 1, "working": 2, "completed": 3, "idle": 4}
	agents := []M{api, web, infra, notes}
	sort.SliceStable(agents, func(i, j int) bool { return rank[agents[i]["status"].(string)] < rank[agents[j]["status"].(string)] })

	attention := []M{}
	feed := []M{{"id": "f0", "sessionId": "d2", "at": cycle - 312000, "kind": "start", "text": "New task started"}}
	if api["status"] == "waiting" {
		attention = append(attention, M{"id": fmt.Sprintf("a1-%d", cycle), "agentId": "d1", "kind": "waiting", "since": cycle + 8000, "name": "api-gateway-3", "project": "api-gateway", "detail": "approval"})
	}
	if t >= 8000 {
		feed = append([]M{{"id": fmt.Sprintf("f1-%d", cycle), "sessionId": "d1", "at": cycle + 8000, "kind": "wait", "text": "Needs approval: Bash"}}, feed...)
	}
	if t >= 14000 {
		attention = append(attention, M{"id": fmt.Sprintf("a3-%d", cycle), "agentId": "d3", "kind": "completed", "since": cycle + 14000, "name": "infra-7", "project": "infra", "detail": 534000})
		feed = append([]M{{"id": fmt.Sprintf("f2-%d", cycle), "sessionId": "d3", "at": cycle + 14000, "kind": "done", "text": "Task completed", "durationMs": 534000}}, feed...)
	}
	if t >= 20000 {
		feed = append([]M{{"id": fmt.Sprintf("f3-%d", cycle), "sessionId": "d1", "at": cycle + 20000, "kind": "info", "text": "Approved: Bash"}}, feed...)
	}
	return M{
		"agents":      agents,
		"attention":   attention,
		"feed":        feed,
		"today":       M{"input": 18.2e6 + grow*21000, "output": 402000 + grow*125, "turns": 37, "sessions": 6, "hookCompletions": 29},
		"integration": M{"registry": true, "hooksSeen": true, "lastHookEventAt": n},
	}
}

// Snapshot returns system, claude and tools sections in the live shapes.
func (s *Source) Snapshot() (system, claude, tools M) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := time.Now().UnixMilli()
	last := func(k string) float64 { h := s.history[k]; return h[len(h)-1] }
	m := func(v any, iv int64, extra M) M {
		out := M{"value": v, "updatedAt": n, "intervalMs": iv}
		for k, x := range extra {
			out[k] = x
		}
		return out
	}
	hist := map[string][]float64{}
	for k, v := range s.history {
		hist[k] = append([]float64(nil), v...)
	}
	perCore := make([]float64, 12)
	for i := range perCore {
		perCore[i] = rand.Float64() * 90
	}
	gib := float64(1 << 30)
	system = M{
		"host": "demo-mac", "cpuModel": "Apple M3 Pro", "cores": 12,
		"cpu":     m(last("cpu"), 1000, M{"perCore": perCore, "load": []float64{3.1, 2.8, 2.6}}),
		"mem":     m(last("mem"), 2000, M{"total": 36 * gib, "used": 36 * gib * last("mem") / 100, "wired": 4.1 * gib, "compressed": 6.3 * gib, "swapBytes": 0}),
		"net":     m(last("netRx")+last("netTx"), 2000, M{"rxBps": last("netRx"), "txBps": last("netTx")}),
		"disk":    m(71, 15000, M{"freeBytes": 288e9, "totalBytes": 994e9}),
		"gpu":     m(last("gpu"), 3000, nil),
		"battery": m(84, 30000, M{"percent": 84, "state": "charging", "source": "ac", "remaining": "0:42"}),
		"thermal": m("nominal", 30000, nil),
		"procs": m([]M{
			{"pid": 1, "name": "node", "cpu": 38.2, "rssBytes": 812e6},
			{"pid": 2, "name": "Google Chrome Helper", "cpu": 21.7, "rssBytes": 1.4e9},
			{"pid": 3, "name": "WindowServer", "cpu": 12.1, "rssBytes": 320e6},
			{"pid": 4, "name": "Code Helper", "cpu": 6.4, "rssBytes": 640e6},
		}, 4000, nil),
		"self":    m(0.8, 5000, M{"rssBytes": 61e6}),
		"history": hist,
	}
	claude = s.story(n)
	tools = M{"tools": []M{
		{"id": "ollama", "label": "Ollama", "status": "loaded", "detail": "1 model loaded", "processes": 2},
		{"id": "codex", "label": "Codex CLI", "status": "running", "detail": "running; task status unavailable", "processes": 1},
	}, "updatedAt": n}
	return
}
