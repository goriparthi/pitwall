package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goriparthi/pitwall/internal/collect/claude"
	"github.com/goriparthi/pitwall/internal/collect/system"
	"github.com/goriparthi/pitwall/internal/collect/tools"
	"github.com/goriparthi/pitwall/internal/config"
	"github.com/goriparthi/pitwall/internal/launcher"
	"github.com/goriparthi/pitwall/internal/logx"
)

func newServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PITWALL_STATE_DIR", filepath.Join(dir, "state"))
	t.Setenv("PITWALL_CLAUDE_DIR", filepath.Join(dir, "claude"))
	t.Setenv("PITWALL_CONFIG", filepath.Join(dir, "config.json"))
	_ = os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"projects":[{"id":"p","name":"P","path":"`+filepath.ToSlash(dir)+`"}],
		"actions":[{"id":"web","label":"Web","kind":"open-url","url":"http://127.0.0.1:1/"}]}`), 0o600)
	store, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	log := logx.New("test")
	s := &Server{Cfg: store, Log: log, Launcher: launcher.New(store.Get), System: system.New(log),
		Claude: claude.New(log, func() int { return 10 }), Tools: &tools.Collector{}}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	return s
}

func do(s *Server, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = "127.0.0.1:7788"
	for k, v := range hdr {
		if k == "host" {
			r.Host = v
		} else {
			r.Header.Set(k, v)
		}
	}
	w := httptest.NewRecorder()
	s.handle(w, r)
	return w
}

func TestHostAndAuthGuards(t *testing.T) {
	s := newServer(t)
	if w := do(s, "GET", "/health", "", map[string]string{"host": "evil.example:7788"}); w.Code != 421 {
		t.Fatalf("rebinding guard: %d", w.Code)
	}
	if w := do(s, "GET", "/health", "", nil); w.Code != 200 {
		t.Fatal("health")
	}
	if w := do(s, "POST", "/v1/ui", `{"focus":true}`, nil); w.Code != 403 {
		t.Fatal("no token")
	}
	if w := do(s, "POST", "/v1/ui", `{}`, map[string]string{"x-pitwall-token": strings.Repeat("0", 48)}); w.Code != 403 {
		t.Fatal("wrong token")
	}
	tok := map[string]string{"x-pitwall-token": s.Token(), "origin": "https://evil.example"}
	if w := do(s, "POST", "/v1/ui", `{}`, tok); w.Code != 403 {
		t.Fatal("foreign origin")
	}
}

func TestUIValidationAndLimits(t *testing.T) {
	s := newServer(t)
	tok := map[string]string{"x-pitwall-token": s.Token(), "origin": "http://127.0.0.1:7788"}
	w := do(s, "POST", "/v1/ui", `{"focus":true,"page":"bogus","project":"nope","brightness":500}`, tok)
	var res struct{ UI UIState }
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != 200 || !res.UI.Focus || res.UI.Page != "balanced" || res.UI.Project != "all" || res.UI.Brightness != 100 {
		t.Fatalf("%d %+v", w.Code, res.UI)
	}
	if w := do(s, "POST", "/v1/ui", strings.Repeat("x", 20000), tok); w.Code != 413 {
		t.Fatalf("size limit: %d", w.Code)
	}
	if w := do(s, "POST", "/v1/ui", `{nope`, tok); w.Code != 400 {
		t.Fatal("bad json")
	}
	if w := do(s, "POST", "/v1/actions/rm-rf/run", `{}`, tok); w.Code != 404 {
		t.Fatal("unknown action")
	}
}

func TestHookIngestionKeepsOnlyMetadata(t *testing.T) {
	s := newServer(t)
	tok := map[string]string{"x-pitwall-token": s.Token()}
	body := `{"hook_event_name":"PermissionRequest","session_id":"sess-1","cwd":"/tmp/x","prompt":"TOP SECRET PROMPT",
		"tool_name":"Bash","tool_input":{"command":"cat ~/.aws/credentials"}}`
	if w := do(s, "POST", "/v1/hooks/claude", body, tok); w.Code != 202 {
		t.Fatalf("ingest %d", w.Code)
	}
	b, _ := json.Marshal(s.Snapshot())
	logged, _ := os.ReadFile(filepath.Join(config.StateDir(), "claude-events.jsonl"))
	all := string(b) + string(logged)
	if strings.Contains(all, "TOP SECRET") || strings.Contains(all, "credentials") {
		t.Fatal("hook content leaked into state or event log")
	}
	snap := s.Claude.Snapshot()
	if len(snap.Agents) != 1 || snap.Agents[0].Status != "waiting" || snap.Agents[0].Wait.Tool != "Bash" || len(snap.Attention) != 1 {
		t.Fatalf("%+v", snap)
	}
}

func TestIndexInjectsTokenWithCSP(t *testing.T) {
	s := newServer(t)
	w := do(s, "GET", "/", "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), s.Token()) || !strings.Contains(w.Header().Get("content-security-policy"), "default-src 'self'") {
		t.Fatal("index")
	}
	if w := do(s, "GET", "/../config.json", "", nil); w.Code != 404 {
		t.Fatal("path traversal")
	}
	_ = http.StatusOK
}
