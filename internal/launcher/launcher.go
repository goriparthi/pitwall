// Package launcher runs only actions declared in config, as argv without a shell.
package launcher

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/goriparthi/pitwall/internal/config"
)

const minInterval = 1500 * time.Millisecond

const macTerminalScript = `on run argv
  tell application "%s"
    activate
    %s
  end tell
end run`

var (
	itermBody    = `set w to (create window with default profile)` + "\n    " + `tell current session of w to write text "cd " & quoted form of (item 1 of argv) & (item 2 of argv)`
	terminalBody = `do script "cd " & quoted form of (item 1 of argv) & (item 2 of argv)`
	winVarRe     = regexp.MustCompile(`%([A-Za-z0-9_]+)%`)
)

// Plan is what an action resolves to; Dir is the working directory when the program takes no path argument.
type Plan struct {
	File string
	Args []string
	Dir  string
}

func expand(s string, p config.Project) string {
	s = strings.ReplaceAll(s, "{project.path}", p.Path)
	return strings.ReplaceAll(s, "{project.name}", p.Name)
}

func expandWinVars(s string) string {
	return winVarRe.ReplaceAllStringFunc(s, func(m string) string { return os.Getenv(m[1 : len(m)-1]) })
}

// Build resolves an action for goos ("darwin" or "windows"). Exported for tests.
func Build(a config.Action, p config.Project, goos string) (Plan, error) {
	switch a.Kind {
	case "open-url":
		u := expand(a.URL, p)
		if goos == "windows" {
			return Plan{File: "rundll32.exe", Args: []string{"url.dll,FileProtocolHandler", u}}, nil
		}
		return Plan{File: "open", Args: []string{u}}, nil
	case "open-app":
		target := ""
		if a.Target != "" {
			target = expand(a.Target, p)
		}
		if goos == "windows" {
			app := expandWinVars(a.App)
			if ext := strings.ToLower(filepath.Ext(app)); ext == ".bat" || ext == ".cmd" {
				return Plan{}, errors.New("batch files are not allowed; point app at an .exe")
			}
			pl := Plan{File: app, Dir: p.Path}
			if target != "" {
				pl.Args = []string{target}
			}
			return pl, nil
		}
		args := []string{"-a", a.App}
		if target != "" {
			args = append(args, target)
		}
		return Plan{File: "open", Args: args}, nil
	case "terminal":
		if p.Path == "" {
			return Plan{}, errors.New("no project directory")
		}
		if goos == "windows" {
			app := expandWinVars(a.App)
			if strings.EqualFold(filepath.Base(app), "wt.exe") || strings.EqualFold(app, "wt") {
				// wt treats ';' as a command separator, so such paths could smuggle a second command
				if strings.Contains(p.Path, ";") {
					return Plan{}, errors.New("project path contains ';', which Windows Terminal would split")
				}
				args := []string{"-d", p.Path}
				if a.Run != "" {
					args = append(args, "cmd.exe", "/k", a.Run) // a.Run comes from the config file only
				}
				return Plan{File: app, Args: args}, nil
			}
			return Plan{File: app, Dir: p.Path}, nil
		}
		var script string
		switch a.App {
		case "iTerm", "iTerm2":
			script = fmt.Sprintf(macTerminalScript, "iTerm", itermBody)
		case "Terminal":
			script = fmt.Sprintf(macTerminalScript, "Terminal", terminalBody)
		default:
			return Plan{File: "open", Args: []string{"-na", a.App, "--args", "--working-directory=" + p.Path}}, nil
		}
		run := ""
		if a.Run != "" {
			run = " && " + a.Run // from the config file only
		}
		// the path travels as an AppleScript argv item and is quoted there, never spliced into the script
		return Plan{File: "osascript", Args: []string{"-e", script, p.Path, run}}, nil
	}
	return Plan{}, errors.New("unsupported action kind " + a.Kind)
}

type Result struct {
	OK     bool   `json:"ok"`
	Status int    `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
}

type Launcher struct {
	cfg  func() *config.Config
	mu   sync.Mutex
	last map[string]time.Time
	Exec func(p Plan) error
	Log  func(msg string, kv map[string]any)
}

func New(cfg func() *config.Config) *Launcher {
	return &Launcher{cfg: cfg, last: map[string]time.Time{}, Exec: execPlan, Log: func(string, map[string]any) {}}
}

func execPlan(p Plan) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, p.File, p.Args...)
	cmd.Dir = p.Dir
	return cmd.Run()
}

func (l *Launcher) Run(actionID, projectID string) Result {
	c := l.cfg()
	var action *config.Action
	for _, a := range c.Actions {
		if a.ID == actionID {
			r := a.Resolve()
			action = &r
			break
		}
	}
	if action == nil {
		return Result{Status: 404, Error: "unknown action"}
	}
	project := pickProject(c.Projects, projectID)
	l.mu.Lock()
	if time.Since(l.last[actionID]) < minInterval {
		l.mu.Unlock()
		return Result{Status: 429, Error: "slow down"}
	}
	l.last[actionID] = time.Now()
	l.mu.Unlock()
	if _, err := os.Stat(project.Path); action.Kind == "terminal" && err != nil {
		return Result{Status: 422, Error: "project path missing: " + project.Name}
	}
	plan, err := Build(*action, project, runtime.GOOS)
	if err != nil {
		return Result{Status: 422, Error: err.Error()}
	}
	if err := l.Exec(plan); err != nil {
		l.Log("action failed", map[string]any{"actionId": actionID, "message": err.Error()})
		return Result{Status: 500, Error: "action failed; see pitwall.log"}
	}
	l.Log("action ran", map[string]any{"actionId": actionID, "project": project.ID})
	return Result{OK: true}
}

// pickProject: the selected project, else the first configured one, else the home directory.
func pickProject(projects []config.Project, id string) config.Project {
	for _, p := range projects {
		if p.ID == id {
			return p
		}
	}
	if len(projects) > 0 {
		return projects[0]
	}
	home, _ := os.UserHomeDir()
	return config.Project{ID: "home", Name: "Home", Path: home}
}

type Item struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Hint  string `json:"hint,omitempty"`
	Key   string `json:"key,omitempty"`
	Kind  string `json:"kind"`
}

func (l *Launcher) List() []Item {
	out := []Item{}
	for _, a := range l.cfg().Actions {
		r := a.Resolve()
		out = append(out, Item{r.ID, r.Label, r.Hint, r.Key, r.Kind})
	}
	return out
}
