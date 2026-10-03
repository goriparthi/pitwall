package launcher

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/goriparthi/pitwall/internal/config"
)

var hostile = config.Project{ID: "p", Name: "Proj", Path: "/tmp/it's; rm -rf ~"}

func TestMacPlansKeepPathsAsArgv(t *testing.T) {
	p, err := Build(config.Action{Kind: "terminal", App: "iTerm", Run: "claude"}, hostile, "darwin")
	if err != nil || p.File != "osascript" || p.Args[2] != hostile.Path || p.Args[3] != " && claude" || !strings.Contains(p.Args[1], "quoted form of (item 1 of argv)") {
		t.Fatalf("%+v %v", p, err)
	}
	p, _ = Build(config.Action{Kind: "open-app", App: "Visual Studio Code", Target: "{project.path}"}, hostile, "darwin")
	if !slices.Equal(p.Args, []string{"-a", "Visual Studio Code", hostile.Path}) {
		t.Fatalf("%+v", p)
	}
}

func TestWindowsPlans(t *testing.T) {
	proj := config.Project{Path: `C:\code\proj`}
	p, _ := Build(config.Action{Kind: "terminal", App: "wt.exe", Run: "claude"}, proj, "windows")
	if p.File != "wt.exe" || !slices.Equal(p.Args, []string{"-d", proj.Path, "cmd.exe", "/k", "claude"}) {
		t.Fatalf("%+v", p)
	}
	if _, err := Build(config.Action{Kind: "terminal", App: "wt.exe"}, config.Project{Path: `C:\a;b`}, "windows"); err == nil {
		t.Fatal("';' must be refused for Windows Terminal")
	}
	if _, err := Build(config.Action{Kind: "open-app", App: `C:\x\run.cmd`}, proj, "windows"); err == nil {
		t.Fatal("batch files must be refused")
	}
	p, _ = Build(config.Action{Kind: "open-url", URL: "https://example.com"}, proj, "windows")
	if p.File != "rundll32.exe" || p.Args[1] != "https://example.com" {
		t.Fatalf("%+v", p)
	}
}

func TestRunAllowlistAndRateLimit(t *testing.T) {
	cfg := &config.Config{Projects: []config.Project{{ID: "p", Name: "P", Path: "/tmp"}},
		Actions: []config.Action{{ID: "web", Label: "Web", Kind: "open-url", URL: "https://example.com"}}}
	l := New(func() *config.Config { return cfg })
	var calls []Plan
	l.Exec = func(p Plan) error { calls = append(calls, p); return nil }
	if r := l.Run("nope", ""); r.Status != 404 {
		t.Fatal("unknown action must 404")
	}
	if r := l.Run("web", ""); !r.OK {
		t.Fatalf("%+v", r)
	}
	if r := l.Run("web", ""); r.Status != 429 {
		t.Fatal("rate limit")
	}
	l.last = map[string]time.Time{}
	l.Exec = func(Plan) error { return errors.New("boom") }
	if r := l.Run("web", ""); r.Status != 500 || strings.Contains(r.Error, "boom") {
		t.Fatal("internal errors are logged, not returned")
	}
	if len(calls) != 1 {
		t.Fatal("exec count")
	}
}
