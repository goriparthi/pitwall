// Package hooks forwards Claude Code hook events and installs/removes the hook entries in settings.json.
package hooks

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/goriparthi/pitwall/internal/collect/claude"
	"github.com/goriparthi/pitwall/internal/config"
)

// Forward reads one hook payload from stdin and posts only sanitized metadata. It never fails loudly:
// Claude Code must not be affected when the dashboard is down.
func Forward(stdin io.Reader) {
	data, err := io.ReadAll(io.LimitReader(stdin, 2<<20))
	if err != nil {
		return
	}
	var raw map[string]any
	if json.Unmarshal(data, &raw) != nil {
		return
	}
	e := claude.Sanitize(raw)
	// re-express in hook field names so the server applies the same Sanitize on receipt
	safe := map[string]any{
		"hook_event_name": e.Event, "session_id": e.SessionID, "cwd": e.Cwd, "transcript_path": e.TranscriptPath,
		"tool_name": e.Tool, "notification_type": e.NotificationType, "source": e.Source, "reason": e.Reason, "error_type": e.Error,
	}
	if e.Target != "" {
		safe["tool_input"] = map[string]any{"file_path": e.Target}
	}
	if e.IsSubagent {
		safe["agent_id"] = "subagent"
	}
	token, err := os.ReadFile(config.TokenFile())
	if err != nil {
		return
	}
	port := os.Getenv("PITWALL_PORT")
	if port == "" {
		port = "7788"
	}
	body, _ := json.Marshal(safe)
	req, err := http.NewRequest("POST", "http://127.0.0.1:"+port+"/v1/hooks/claude", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-pitwall-token", strings.TrimSpace(string(token)))
	client := http.Client{Timeout: 600 * time.Millisecond}
	if res, err := client.Do(req); err == nil {
		res.Body.Close()
	}
}
