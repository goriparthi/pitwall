package claude

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func ev(event string, at int64, mod func(*Event)) Event {
	e := Event{Event: event, SessionID: "s", At: at}
	if mod != nil {
		mod(&e)
	}
	return e
}

func TestLifecycle(t *testing.T) {
	s := &Session{}
	Apply(s, ev("UserPromptSubmit", 1000, nil))
	Apply(s, ev("PreToolUse", 2000, func(e *Event) { e.Tool, e.Target = "Edit", "app.js" }))
	if s.Phase != "working" || s.LastEvent != "Edit app.js" {
		t.Fatalf("%+v", s)
	}
	if f := Apply(s, ev("PermissionRequest", 3000, func(e *Event) { e.Tool = "Bash" })); s.Phase != "waiting" || f.Kind != "wait" || s.Wait.Tool != "Bash" {
		t.Fatalf("approval wait: %+v", s)
	}
	Apply(s, ev("PostToolUse", 4000, nil))
	if s.Phase != "working" {
		t.Fatal("tool result should resume working")
	}
	if f := Apply(s, ev("Stop", 9000, nil)); s.Phase != "completed" || f.DurationMs != 8000 {
		t.Fatalf("stop: %+v %+v", s, f)
	}
}

func TestQuestionsSubagentsFailures(t *testing.T) {
	s := &Session{}
	Apply(s, ev("UserPromptSubmit", 1, nil))
	Apply(s, ev("PreToolUse", 2, func(e *Event) { e.Tool = "AskUserQuestion" }))
	if s.Phase != "waiting" || s.Wait.Kind != "question" {
		t.Fatal("question should wait")
	}
	Apply(s, ev("PostToolUse", 3, nil))
	Apply(s, ev("PreToolUse", 4, func(e *Event) { e.Tool, e.IsSubagent = "Read", true }))
	if s.LastEvent != "Prompt submitted" {
		t.Fatal("subagent tool must not move the main session")
	}
	Apply(s, ev("PreToolUse", 5, func(e *Event) { e.Tool = "ExitPlanMode" }))
	if s.Wait.Kind != "plan approval" {
		t.Fatal("plan approval")
	}
	Apply(s, ev("StopFailure", 6, func(e *Event) { e.Error = "rate_limit" }))
	if s.Phase != "failed" || s.Error != "rate_limit" {
		t.Fatal("failure")
	}
	Apply(s, ev("UserPromptSubmit", 7, nil))
	if s.Phase != "working" || s.Error != "" {
		t.Fatal("new prompt clears failure")
	}
}

func TestFuse(t *testing.T) {
	h := &Session{Phase: "working", LastHookAt: 1000}
	if st, _ := Fuse(h, &Registry{Status: "idle", StatusUpdatedAt: 2000}, 3000, 60000); st != "idle" {
		t.Fatal("registry idle after last hook must win (Esc interrupt)")
	}
	if st, _ := Fuse(h, &Registry{Status: "idle", StatusUpdatedAt: 500}, 3000, 60000); st != "working" {
		t.Fatal("older registry idle must not override")
	}
	done := &Session{Phase: "completed", LastHookAt: 1000, CompletedAt: 1000}
	if st, _ := Fuse(done, nil, 5000, 10000); st != "completed" {
		t.Fatal("completed hold")
	}
	if st, _ := Fuse(done, nil, 20000, 10000); st != "idle" {
		t.Fatal("completed expires to idle")
	}
	if st, src := Fuse(nil, &Registry{Status: "busy"}, 0, 0); st != "working" || src != "registry" {
		t.Fatal("registry only")
	}
	if st, _ := Fuse(nil, nil, 0, 0); st != "unknown" {
		t.Fatal("nothing known")
	}
}

func TestSanitizeDropsContent(t *testing.T) {
	var raw map[string]any
	_ = json.Unmarshal([]byte(`{"hook_event_name":"PreToolUse","session_id":"s1","cwd":"/x/p","prompt":"SECRET PROMPT",
		"tool_name":"Write","tool_input":{"file_path":"/x/p/.env","content":"API_KEY=abc","command":"rm -rf /"},
		"tool_response":{"output":"secret output"}}`), &raw)
	b, _ := json.Marshal(Sanitize(raw))
	for _, bad := range []string{"SECRET PROMPT", "API_KEY", "rm -rf", "secret output", "/x/p/.env"} {
		if strings.Contains(string(b), bad) {
			t.Fatalf("leaked %q: %s", bad, b)
		}
	}
	if e := Sanitize(raw); e.Target != ".env" {
		t.Fatal("basename only")
	}
}

func TestPrettyTool(t *testing.T) {
	for in, want := range map[string]string{
		"mcp__plugin_chrome-devtools-mcp_chrome-devtools__take_screenshot": "take_screenshot · chrome-devtools",
		"mcp__github__create_issue": "create_issue · github",
		"Bash":                      "Bash",
	} {
		if got := PrettyTool(in); got != want {
			t.Errorf("%s -> %s", in, got)
		}
	}
}

func TestUsageDedupe(t *testing.T) {
	a := NewAccumulator()
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	u := `{"input_tokens":10,"cache_read_input_tokens":1000,"cache_creation_input_tokens":90,"output_tokens":50}`
	lines := []string{
		`{"type":"assistant","timestamp":"` + ts + `","message":{"id":"m1","model":"claude-opus-5-5","usage":` + u + `}}`,
		`{"type":"assistant","timestamp":"` + ts + `","message":{"id":"m1","model":"claude-opus-5-5","stop_reason":"end_turn","usage":` + u + `}}`,
		`{"type":"assistant","timestamp":"` + ts + `","isSidechain":true,"message":{"id":"s1","model":"claude-haiku-4-5","usage":{"input_tokens":5,"output_tokens":7}}}`,
		`{"type":"ai-title","aiTitle":"Fix login bug"}`,
		`{"type":"user","message":{"content":"never parsed for content"}}`,
	}
	for _, l := range lines {
		a.Line([]byte(l))
	}
	g := a.U
	if g.InputTokens != 1105 || g.OutputTokens != 57 || g.ContextTokens != 1150 || g.Turns != 1 || g.Title != "Fix login bug" || g.Model != "claude-opus-5-5" {
		t.Fatalf("%+v", g)
	}
	if g.TodayOutput != 57 || g.TodayTurns != 1 {
		t.Fatalf("today %+v", g)
	}
}

func TestSlug(t *testing.T) {
	if ProjectSlug("/Users/me/code/pitwall") != "-Users-me-code-pitwall" {
		t.Fatal("slug")
	}
}
