// Package claude turns Claude Code's session registry, hook events and transcript usage into agent status.
// Only display-safe metadata is kept: no prompts, commands, tool inputs or outputs.
package claude

import (
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Event is a sanitized hook payload.
type Event struct {
	Event            string `json:"event"`
	SessionID        string `json:"sessionId"`
	Cwd              string `json:"cwd,omitempty"`
	TranscriptPath   string `json:"transcriptPath,omitempty"`
	Tool             string `json:"tool,omitempty"`
	Target           string `json:"target,omitempty"`
	NotificationType string `json:"notificationType,omitempty"`
	Source           string `json:"source,omitempty"`
	Reason           string `json:"reason,omitempty"`
	IsSubagent       bool   `json:"isSubagent,omitempty"`
	Error            string `json:"error,omitempty"`
	At               int64  `json:"at"`
}

var mcpRe = regexp.MustCompile(`^mcp__(.+?)__(.+)$`)

// PrettyTool: mcp__plugin_chrome-devtools-mcp_chrome-devtools__take_screenshot -> "take_screenshot · chrome-devtools".
func PrettyTool(name string) string {
	if name == "" {
		return ""
	}
	m := mcpRe.FindStringSubmatch(name)
	if m == nil {
		return trunc(name, 60)
	}
	parts := strings.Split(strings.TrimPrefix(m[1], "plugin_"), "_")
	return trunc(m[2]+" · "+parts[len(parts)-1], 60)
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func str(v any, n int) string {
	s, _ := v.(string)
	return trunc(s, n)
}

// Sanitize keeps metadata only; tool inputs are reduced to a file basename.
func Sanitize(raw map[string]any) Event {
	e := Event{
		Event:            str(raw["hook_event_name"], 40),
		SessionID:        str(raw["session_id"], 64),
		Cwd:              str(raw["cwd"], 400),
		TranscriptPath:   str(raw["transcript_path"], 600),
		Tool:             PrettyTool(str(raw["tool_name"], 200)),
		NotificationType: str(raw["notification_type"], 40),
		Source:           str(raw["source"], 20),
		Reason:           str(raw["reason"], 40),
		IsSubagent:       raw["agent_id"] != nil && raw["agent_id"] != "",
		At:               time.Now().UnixMilli(),
	}
	if in, ok := raw["tool_input"].(map[string]any); ok {
		for _, k := range []string{"file_path", "notebook_path", "path"} {
			if f := str(in[k], 400); f != "" {
				e.Target = filepath.Base(strings.ReplaceAll(f, `\`, "/"))
				break
			}
		}
	}
	switch v := raw["error"].(type) {
	case string:
		e.Error = trunc(v, 40)
	case map[string]any:
		e.Error = str(v["type"], 40)
	}
	if e.Error == "" {
		e.Error = str(raw["error_type"], 40)
	}
	if e.Error == "" && e.Event == "StopFailure" {
		e.Error = "api_error"
	}
	return e
}

type Wait struct {
	Kind  string `json:"kind"`
	Tool  string `json:"tool,omitempty"`
	Since int64  `json:"since"`
}

// Session is per-session hook-derived state.
type Session struct {
	Phase          string
	LastHookAt     int64
	Cwd            string
	TranscriptPath string
	StartedAt      int64
	TurnStartedAt  int64
	CompletedAt    int64
	FailedAt       int64
	LastTurnMs     int64
	LastEvent      string
	Wait           *Wait
	Error          string
	Subagents      int
}

type FeedItem struct {
	Kind       string `json:"kind"`
	Text       string `json:"text"`
	DurationMs int64  `json:"durationMs,omitempty"`
}

var waitTools = map[string]bool{"AskUserQuestion": true, "ExitPlanMode": true}

// Apply advances one session's state for an event; returns a feed item for meaningful transitions.
func Apply(s *Session, e Event) *FeedItem {
	s.LastHookAt = e.At
	if e.Cwd != "" {
		s.Cwd = e.Cwd
	}
	if e.TranscriptPath != "" {
		s.TranscriptPath = e.TranscriptPath
	}
	label := e.Tool
	if label != "" && e.Target != "" {
		label += " " + e.Target
	}
	switch e.Event {
	case "SessionStart":
		s.Phase = "idle"
		if s.StartedAt == 0 {
			s.StartedAt = e.At
		}
		if e.Source == "resume" {
			return &FeedItem{Kind: "session", Text: "Session resumed"}
		}
		return &FeedItem{Kind: "session", Text: "Session started"}
	case "UserPromptSubmit":
		s.Phase, s.TurnStartedAt, s.Wait, s.Error, s.LastEvent = "working", e.At, nil, "", "Prompt submitted"
		return &FeedItem{Kind: "start", Text: "New task started"}
	case "PreToolUse":
		if e.IsSubagent {
			return nil
		}
		if waitTools[e.Tool] {
			s.Phase = "waiting"
			if e.Tool == "ExitPlanMode" {
				s.Wait = &Wait{Kind: "plan approval", Since: e.At}
				return &FeedItem{Kind: "wait", Text: "Plan ready for review"}
			}
			s.Wait = &Wait{Kind: "question", Since: e.At}
			return &FeedItem{Kind: "wait", Text: "Asked you a question"}
		}
		s.Phase, s.Wait, s.LastEvent = "working", nil, label
		return nil
	case "PostToolUse", "PostToolUseFailure":
		if e.IsSubagent {
			return nil
		}
		if s.Phase == "waiting" {
			s.Phase = "working"
		}
		s.Wait = nil
		if e.Event == "PostToolUseFailure" {
			s.LastEvent = label + " failed"
			return &FeedItem{Kind: "tool-fail", Text: e.Tool + " failed"}
		}
		return nil
	case "PermissionRequest":
		s.Phase, s.Wait = "waiting", &Wait{Kind: "approval", Tool: e.Tool, Since: e.At}
		return &FeedItem{Kind: "wait", Text: "Needs approval: " + orDefault(e.Tool, "tool")}
	case "PermissionDenied":
		return &FeedItem{Kind: "info", Text: "Denied: " + orDefault(e.Tool, "tool")}
	case "Notification":
		switch e.NotificationType {
		case "permission_prompt":
			s.Phase = "waiting"
			if s.Wait == nil {
				s.Wait = &Wait{Kind: "approval", Since: e.At}
			}
			return &FeedItem{Kind: "wait", Text: "Waiting for approval"}
		case "elicitation_dialog":
			s.Phase, s.Wait = "waiting", &Wait{Kind: "input", Since: e.At}
			return &FeedItem{Kind: "wait", Text: "Waiting for input"}
		}
		return nil
	case "SubagentStart":
		s.Subagents++
		return nil
	case "SubagentStop":
		if s.Subagents > 0 {
			s.Subagents--
		}
		return nil
	case "Stop":
		if e.IsSubagent {
			return nil
		}
		var dur int64
		if s.TurnStartedAt > 0 {
			dur = e.At - s.TurnStartedAt
		}
		s.Phase, s.CompletedAt, s.LastTurnMs, s.Wait, s.Subagents = "completed", e.At, dur, nil, 0
		return &FeedItem{Kind: "done", Text: "Task completed", DurationMs: dur}
	case "StopFailure":
		s.Phase, s.Error, s.FailedAt, s.Wait = "failed", orDefault(e.Error, "api_error"), e.At, nil
		return &FeedItem{Kind: "fail", Text: "Failed: " + strings.ReplaceAll(s.Error, "_", " ")}
	case "SessionEnd":
		s.Phase = "ended"
		return &FeedItem{Kind: "session", Text: "Session ended"}
	}
	return nil
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// Registry is one ~/.claude/sessions/<pid>.json entry for a live process.
type Registry struct {
	PID             int    `json:"pid"`
	SessionID       string `json:"sessionId"`
	Cwd             string `json:"cwd"`
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	Status          string `json:"status"`
	StartedAt       int64  `json:"startedAt"`
	StatusUpdatedAt int64  `json:"statusUpdatedAt"`
	UpdatedAt       int64  `json:"updatedAt"`
}

// Fuse reconciles hook phase with registry busy/idle. Interrupts (Esc) emit no Stop hook, so a registry
// idle newer than the last hook wins over "working".
func Fuse(h *Session, r *Registry, now, holdMs int64) (status, source string) {
	reg := ""
	if r != nil {
		switch r.Status {
		case "busy":
			reg = "working"
		case "idle":
			reg = "idle"
		default:
			reg = "unknown"
		}
	}
	if h == nil || h.Phase == "" {
		if reg == "" {
			return "unknown", "none"
		}
		return reg, "registry"
	}
	switch h.Phase {
	case "waiting", "failed":
		return h.Phase, "hooks"
	case "working":
		if reg == "idle" && r.StatusUpdatedAt > h.LastHookAt {
			return "idle", "registry"
		}
		return "working", "hooks"
	}
	if reg == "working" && r.StatusUpdatedAt > h.LastHookAt {
		return "working", "registry"
	}
	if h.Phase == "completed" && now-h.CompletedAt < holdMs {
		return "completed", "hooks"
	}
	return "idle", "hooks"
}
