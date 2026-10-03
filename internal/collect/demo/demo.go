// Package demo produces synthetic, clearly labelled data for previewing the design.
package demo

import (
	"math"
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

// Snapshot returns system, claude and tools sections in the live shapes.
func (s *Source) Snapshot() (system, claude, tools M) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := time.Now().UnixMilli()
	b := s.t0.UnixMilli() // anchored so attention items keep a stable identity
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
	agent := func(id, name, project, status, src string, since int64, task, ev string, extra M) M {
		a := M{"id": id, "tool": "claude-code", "label": "Claude Code", "name": name, "project": project, "status": status, "statusSource": src, "since": since, "task": task, "lastEvent": ev, "hooks": src == "hooks", "subagents": 0}
		for k, v := range extra {
			a[k] = v
		}
		return a
	}
	u := func(model string, ctx, in, out float64, turns int) M {
		return M{"model": model, "contextTokens": ctx, "inputTokens": in, "outputTokens": out, "turns": turns}
	}
	claude = M{
		"agents": []M{
			agent("d1", "api-gateway-3", "api-gateway", "waiting", "hooks", b-47000, "Migrate rate limiter to sliding window", "Bash", M{"wait": M{"kind": "approval", "tool": "Bash"}, "usage": u("claude-opus-5-5", 88400, 2.1e6, 41200, 6)}),
			agent("d2", "web-dashboard-1", "web-dashboard", "working", "hooks", b-312000, "Add dark mode tokens to settings page", "Edit theme.css", M{"subagents": 2, "usage": u("claude-sonnet-5-5", 61200, 1.3e6, 28800, 4)}),
			agent("d3", "infra-7", "infra", "completed", "hooks", b-140000, "Tighten S3 bucket policies", "Read main.tf", M{"lastTurnMs": 534000, "usage": u("claude-opus-5-5", 120300, 3.4e6, 66100, 11)}),
			agent("d4", "notes-2", "notes", "idle", "registry", b-3600000, "Weekly summary draft", "", M{"usage": nil}),
		},
		"attention": []M{
			{"id": "a1", "agentId": "d1", "kind": "waiting", "since": b - 47000, "name": "api-gateway-3", "project": "api-gateway", "detail": "approval"},
			{"id": "a3", "agentId": "d3", "kind": "completed", "since": b - 140000, "name": "infra-7", "project": "infra", "detail": 534000},
		},
		"feed": []M{
			{"id": "f1", "sessionId": "d1", "at": b - 47000, "kind": "wait", "text": "Needs approval: Bash"},
			{"id": "f2", "sessionId": "d3", "at": b - 140000, "kind": "done", "text": "Task completed", "durationMs": 534000},
			{"id": "f3", "sessionId": "d2", "at": b - 312000, "kind": "start", "text": "New task started"},
		},
		"today":       M{"input": 18.2e6, "output": 402000, "turns": 37, "sessions": 6, "hookCompletions": 29},
		"integration": M{"registry": true, "hooksSeen": true, "lastHookEventAt": b - 47000},
	}
	tools = M{"tools": []M{
		{"id": "ollama", "label": "Ollama", "status": "loaded", "detail": "1 model loaded", "processes": 2},
		{"id": "codex", "label": "Codex CLI", "status": "running", "detail": "running; task status unavailable", "processes": 1},
	}, "updatedAt": n}
	return
}
