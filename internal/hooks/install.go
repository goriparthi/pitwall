package hooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/goriparthi/pitwall/internal/collect/claude"
)

var Events = []string{"SessionStart", "SessionEnd", "UserPromptSubmit", "PreToolUse", "PostToolUse", "PostToolUseFailure",
	"PermissionRequest", "Notification", "Stop", "StopFailure", "SubagentStart", "SubagentStop"}

// Marker identifies our entries: the forwarder command always ends with this subcommand.
const Marker = " hook"

func SettingsPath() string {
	if p := os.Getenv("PITWALL_CLAUDE_SETTINGS"); p != "" {
		return p
	}
	return filepath.Join(claude.ClaudeDir(), "settings.json")
}

// ours matches entries from this installer, including ones from the earlier names (Node forwarder, mission-control).
func ours(h map[string]any, exe string) bool {
	cmd, _ := h["command"].(string)
	if exe != "" && strings.Contains(cmd, exe) && strings.HasSuffix(cmd, Marker) {
		return true
	}
	if strings.Contains(cmd, "claude-hook.mjs") {
		return true
	}
	for _, name := range []string{"pitwall", "mission-control"} {
		if strings.HasSuffix(cmd, name+"\""+Marker) || strings.HasSuffix(cmd, name+Marker) {
			return true
		}
	}
	return false
}

func strip(settings map[string]any, exe string) {
	hooks, _ := settings["hooks"].(map[string]any)
	for ev, v := range hooks {
		groups, _ := v.([]any)
		var keep []any
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			if gm == nil {
				keep = append(keep, g)
				continue
			}
			list, _ := gm["hooks"].([]any)
			var hs []any
			for _, h := range list {
				if hm, _ := h.(map[string]any); hm == nil || !ours(hm, exe) {
					hs = append(hs, h)
				}
			}
			if len(hs) > 0 {
				gm["hooks"] = hs
				keep = append(keep, gm)
			}
		}
		if len(keep) == 0 {
			delete(hooks, ev)
		} else {
			hooks[ev] = keep
		}
	}
}

func load() (map[string]any, error) {
	data, err := os.ReadFile(SettingsPath())
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON; not touching it: %w", SettingsPath(), err)
	}
	return m, nil
}

func save(m map[string]any) (string, error) {
	p := SettingsPath()
	backup := ""
	if _, err := os.Stat(p); err == nil {
		backup = p + ".pitwall-backup-" + time.Now().UTC().Format("20060102T150405Z")
		data, _ := os.ReadFile(p)
		if err := os.WriteFile(backup, data, 0o600); err != nil {
			return "", err
		}
	}
	data, _ := json.MarshalIndent(m, "", "  ")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return "", err
	}
	return backup, os.Rename(tmp, p)
}

// Install replaces any previous Pitwall entries (Node or Go) with the given binary.
func Install(exe string) (string, error) {
	if strings.Contains(exe, "go-build") {
		return "", errors.New("run install from a built binary, not `go run`")
	}
	m, err := load()
	if err != nil {
		return "", err
	}
	strip(m, exe)
	hooks, _ := m["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		m["hooks"] = hooks
	}
	cmd := fmt.Sprintf("%q%s", exe, Marker)
	for _, ev := range Events {
		list, _ := hooks[ev].([]any)
		hooks[ev] = append(list, map[string]any{"matcher": "*", "hooks": []any{map[string]any{"type": "command", "command": cmd, "async": true, "timeout": 5}}})
	}
	return save(m)
}

func Uninstall(exe string) (string, error) {
	m, err := load()
	if err != nil {
		return "", err
	}
	strip(m, exe)
	return save(m)
}

func Status(exe string) []string {
	m, err := load()
	if err != nil {
		return nil
	}
	var found []string
	hooks, _ := m["hooks"].(map[string]any)
	for _, ev := range Events {
		groups, _ := hooks[ev].([]any)
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			list, _ := gm["hooks"].([]any)
			for _, h := range list {
				if hm, _ := h.(map[string]any); hm != nil && ours(hm, exe) {
					found = append(found, ev)
				}
			}
		}
	}
	return found
}
