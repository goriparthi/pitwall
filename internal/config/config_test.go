package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefaultsParse(t *testing.T) {
	c, err := Parse(defaultJSON)
	if err != nil {
		t.Fatal(err)
	}
	if c.Server.Port != 7788 || c.Display.FPS != 6 || len(c.Actions) != 5 {
		t.Fatalf("%+v", c)
	}
}

func TestValidation(t *testing.T) {
	_, err := Parse([]byte(`{"server":{"host":"0.0.0.0"},"actions":[
		{"id":"a","label":"A","kind":"shell","key":"x"},
		{"id":"b","label":"B","kind":"open-url","url":"file:///etc/passwd"},
		{"id":"c","label":"C","kind":"open-app","app":"X","key":"f"}],
		"layouts":[{"id":"bad","name":"Bad","columns":["100%"],"slots":["nope"]}]}`))
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{"loopback", "kind must be", "url must be http", "reserved", "unknown widget", "must look like"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %v", want, err)
		}
	}
}

func TestOSOverride(t *testing.T) {
	a := Action{ID: "t", Kind: "terminal", Mac: &Action{App: "iTerm"}, Windows: &Action{App: "wt.exe"}}
	r := a.Resolve()
	if r.App == "" || r.Mac != nil || r.Windows != nil {
		t.Fatalf("%+v", r)
	}
}

func TestLayoutsOverrideAndOrder(t *testing.T) {
	c := &Config{Layouts: []Layout{
		{ID: "balanced", Name: "Mine", Columns: []string{"1fr"}, Slots: []string{"ai"}},
		{ID: "custom", Name: "Custom", Columns: []string{"1fr"}, Slots: []string{"usage"}},
	}, UI: UI{Layouts: []string{"custom", "missing", "system"}}}
	all := c.AllLayouts()
	if all[0].Name != "Mine" || all[len(all)-1].ID != "custom" || len(all) != len(BuiltinLayouts)+1 {
		t.Fatalf("%+v", all)
	}
	if got := c.PageOrder(); strings.Join(got, ",") != "custom,system" {
		t.Fatalf("order %v", got)
	}
}

func TestForIntegrationsHidesWhatIsAbsent(t *testing.T) {
	all := (&Config{}).AllLayouts()
	without := ForIntegrations(all, map[string]bool{})
	ids := map[string]Layout{}
	for _, l := range without {
		ids[l.ID] = l
	}
	if _, ok := ids["ai-desk"]; ok {
		t.Fatal("ai-desk requires redline and must be hidden without it")
	}
	if _, ok := ids["ops-desk"]; ok {
		t.Fatal("ops-desk requires ops and must be hidden without a configured source")
	}
	if u := ids["usage"]; strings.Join(u.Slots, ",") != "usage" || strings.Join(u.Columns, ",") != "1fr" {
		t.Fatalf("limits slot and its column should be dropped: %+v", u)
	}
	with := ForIntegrations(all, map[string]bool{"redline": true, "ops": true})
	if len(with) != len(all) {
		t.Fatal("everything is usable when every integration is present")
	}
}

func TestOpsSourceValidation(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "status")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	base := `{"ops":{"sources":[%s]}}`
	ok := fmt.Sprintf(`{"id":"p","command":[%q,"--window","30"]}`, exe)
	c, err := Parse([]byte(fmt.Sprintf(base, ok)))
	if err != nil {
		t.Fatal(err)
	}
	if s := c.Ops.Sources[0]; s.Label != "p" || s.IntervalSeconds != 120 || s.TimeoutSeconds != 30 {
		t.Fatalf("defaults not applied: %+v", s)
	}
	bad := map[string]string{
		"relative path":       `{"id":"p","command":["status"]}`,
		"missing file":        fmt.Sprintf(`{"id":"p","command":[%q]}`, filepath.Join(dir, "nope")),
		"no command":          `{"id":"p","command":[]}`,
		"timeout >= interval": fmt.Sprintf(`{"id":"p","command":[%q],"intervalSeconds":30,"timeoutSeconds":60}`, exe),
		"duplicate id":        ok + "," + ok,
		"bad id":              fmt.Sprintf(`{"id":"a b","command":[%q]}`, exe),
	}
	if runtime.GOOS != "windows" {
		ww := filepath.Join(dir, "open")
		_ = os.WriteFile(ww, nil, 0o755)
		_ = os.Chmod(ww, 0o757)
		bad["world writable"] = fmt.Sprintf(`{"id":"p","command":[%q]}`, ww)
	}
	for name, src := range bad {
		if _, err := Parse([]byte(fmt.Sprintf(base, src))); err == nil {
			t.Errorf("%s: expected a validation error", name)
		}
	}
}
