package hooks

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestForwardSendsOnlyMetadata(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PITWALL_STATE_DIR", dir)
	_ = os.WriteFile(filepath.Join(dir, "token"), []byte(strings.Repeat("a", 48)), 0o600)
	got := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- r.Header.Get("x-pitwall-token") + "|" + string(b)
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	t.Setenv("PITWALL_PORT", port)
	Forward(strings.NewReader(`{"hook_event_name":"PreToolUse","session_id":"s","prompt":"SECRET","tool_name":"Write",
		"tool_input":{"file_path":"/a/b/.env","content":"API_KEY=1"}}`))
	select {
	case msg := <-got:
		if strings.Contains(msg, "SECRET") || strings.Contains(msg, "API_KEY") || strings.Contains(msg, "/a/b") || !strings.HasPrefix(msg, strings.Repeat("a", 48)) {
			t.Fatalf("forwarded %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nothing forwarded")
	}
}

func TestForwardSilentWhenServerDown(t *testing.T) {
	t.Setenv("PITWALL_STATE_DIR", t.TempDir())
	t.Setenv("PITWALL_PORT", "1")
	start := time.Now()
	Forward(strings.NewReader(`{"hook_event_name":"Stop","session_id":"x"}`))
	if time.Since(start) > 2*time.Second {
		t.Fatal("forwarder must give up quickly")
	}
}

func TestInstallIdempotentAndRoundTrips(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	t.Setenv("PITWALL_CLAUDE_SETTINGS", p)
	orig := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"~/.claude/hooks/gitleaks.sh"}]}],
		"Stop":[{"matcher":"*","hooks":[{"type":"command","command":"node /x/hooks/claude-hook.mjs"}]}]},"other":1}`
	_ = os.WriteFile(p, []byte(orig), 0o600)
	exe := "/opt/mc/pitwall"
	for i := 0; i < 2; i++ {
		if _, err := Install(exe); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(Status(exe)); n != len(Events) {
		t.Fatalf("installed for %d events", n)
	}
	raw, _ := os.ReadFile(p)
	if strings.Contains(string(raw), "claude-hook.mjs") || !strings.Contains(string(raw), "gitleaks.sh") {
		t.Fatal("old Node entry must be replaced and other hooks kept")
	}
	if _, err := Uninstall(exe); err != nil {
		t.Fatal(err)
	}
	var after, want map[string]any
	raw, _ = os.ReadFile(p)
	_ = json.Unmarshal(raw, &after)
	_ = json.Unmarshal([]byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"~/.claude/hooks/gitleaks.sh"}]}]},"other":1}`), &want)
	a, _ := json.Marshal(after)
	w, _ := json.Marshal(want)
	if string(a) != string(w) {
		t.Fatalf("uninstall left %s", a)
	}
}
