package ops

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/goriparthi/pitwall/internal/config"
)

func TestParse(t *testing.T) {
	now := time.Date(2026, 10, 3, 14, 0, 0, 0, time.UTC)
	doc := `{"schema":"pitwall.status/1","asOf":"2026-10-03T13:58:00Z","summary":"all clear",
		"items":[{"key":"ingest","label":"File ingest","status":"OK","value":"12 pending","detail":"oldest 2m"}],
		"findings":[{"status":"bogus","text":"line\nbreak"}]}`
	r, err := Parse([]byte(doc), 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "warn" {
		t.Errorf("exit 1 without a status should be warn, got %s", r.Status)
	}
	if r.AsOf != time.Date(2026, 10, 3, 13, 58, 0, 0, time.UTC).UnixMilli() {
		t.Errorf("asOf not parsed: %d", r.AsOf)
	}
	if r.Items[0].Status != "ok" || r.Findings[0].Status != "unknown" || strings.Contains(r.Findings[0].Text, "\n") {
		t.Errorf("normalization failed: %+v", r)
	}

	r, _ = Parse([]byte(`{"schema":"pitwall.status/1","status":"crit"}`), 0, now)
	if r.Status != "crit" || r.AsOf != now.UnixMilli() || r.Items == nil || r.Findings == nil {
		t.Errorf("explicit status and defaults: %+v", r)
	}

	many := `{"schema":"pitwall.status/1","summary":"` + strings.Repeat("x", 200) + `","items":[` + strings.Repeat(`{"key":"k"},`, 20) + `{"key":"k"}]}`
	r, _ = Parse([]byte(many), 0, now)
	if len(r.Items) != maxItems || len([]rune(r.Summary)) != 80 {
		t.Errorf("caps not applied: %d items, summary %d", len(r.Items), len([]rune(r.Summary)))
	}

	withCharts := `{"schema":"pitwall.status/1","charts":[
		{"key":"depth","label":"Queue","kind":"stacked-area","series":[{"name":"A","kind":"line","color":"#ABCDEF","points":[[1,2],[3,4]]},{"name":"B","color":"red","points":[]}]},
		{"key":"bad","kind":"pie","series":[{"name":"x","points":[[1,1]]}]},
		{"key":"flow","kind":"bars-line","series":[{"name":"In","kind":"bar","points":[[1,5]]},{"name":"Out","kind":"line","points":[[1,4]]},{"name":"?","kind":"spline"}]}]}`
	r, err = Parse([]byte(withCharts), 0, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Charts) != 2 || r.Charts[0].Series[0].Kind != "area" || r.Charts[0].Series[0].Color != "#abcdef" || r.Charts[0].Series[1].Color != "" {
		t.Errorf("stacked-area forces areas and only hex colors pass: %+v", r.Charts)
	}
	if f := r.Charts[1]; f.Key != "flow" || len(f.Series) != 2 || f.Series[1].Kind != "line" {
		t.Errorf("unknown chart and series kinds are dropped: %+v", f)
	}

	for name, c := range map[string]struct {
		doc  string
		code int
		want string
	}{
		"exit 3":       {`{"schema":"pitwall.status/1"}`, 3, "exit 3"},
		"not json":     {`ok`, 0, "invalid json"},
		"wrong schema": {`{"schema":"other/1"}`, 0, "invalid json"},
		"bad asOf":     {`{"schema":"pitwall.status/1","asOf":"yesterday"}`, 0, "invalid json"},
	} {
		if _, err := Parse([]byte(c.doc), c.code, now); err == nil || err.Error() != c.want {
			t.Errorf("%s: got %v want %s", name, err, c.want)
		}
	}
}

func script(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "status")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts")
	}
	ctx := context.Background()
	ok := script(t, `echo '{"schema":"pitwall.status/1","summary":"fine"}'; exit 2`)
	r, errClass, _ := run(ctx, config.OpsSource{Command: []string{ok}, TimeoutSeconds: 5})
	if errClass != "" || r.Status != "crit" || r.Summary != "fine" {
		t.Errorf("exit 2 should read as crit: %q %+v", errClass, r)
	}

	// the child sleep must die with the group, or the run would wait for it
	slow := script(t, `sleep 30 & sleep 30`)
	start := time.Now()
	_, errClass, _ = run(ctx, config.OpsSource{Command: []string{slow}, TimeoutSeconds: 1})
	if errClass != "timeout" || time.Since(start) > 5*time.Second {
		t.Errorf("timeout: got %q after %s", errClass, time.Since(start))
	}

	big := script(t, `head -c 70000 /dev/zero | tr '\0' 'x'`)
	if _, errClass, _ = run(ctx, config.OpsSource{Command: []string{big}, TimeoutSeconds: 5}); errClass != "output too large" {
		t.Errorf("large output: got %q", errClass)
	}

	if _, errClass, _ = run(ctx, config.OpsSource{Command: []string{"/nonexistent/status"}, TimeoutSeconds: 5}); errClass != "not found" {
		t.Errorf("missing command: got %q", errClass)
	}
}

func TestSnapshotStaleFailingAndReload(t *testing.T) {
	cfg := &config.Config{Ops: config.Ops{Sources: []config.OpsSource{{ID: "a", Label: "A", Command: []string{"/x"}, IntervalSeconds: 60, TimeoutSeconds: 10}}}}
	c := &Collector{Cfg: func() *config.Config { return cfg }, state: map[string]*state{}}
	now := time.Now()
	c.state["a"] = &state{src: Source{ID: "a", HasReading: true, Reading: Reading{Status: "crit", AsOf: now.Add(-1 * time.Minute).UnixMilli()}}}
	s := c.Snapshot(now)
	if !s.Present || s.Sources[0].Stale || !s.Critical() {
		t.Fatalf("fresh crit should light: %+v", s)
	}
	if s = c.Snapshot(now.Add(3 * time.Minute)); !s.Sources[0].Stale || s.Critical() {
		t.Fatalf("a reading older than 2x interval + timeout is stale and must not light: %+v", s)
	}
	c.state["a"].src.Failures = failingRuns
	if s = c.Snapshot(now); !s.Sources[0].Failing || s.Critical() {
		t.Fatalf("failing source must not light: %+v", s)
	}

	// a changed command is due at once instead of waiting out the old interval
	c.state["a"].next, c.state["a"].conf = now.Add(time.Hour), confKey(cfg.Ops.Sources[0])
	cfg = &config.Config{Ops: config.Ops{Sources: []config.OpsSource{{ID: "a", Label: "A", Command: []string{"/y"}, IntervalSeconds: 60, TimeoutSeconds: 10}}}}
	c.state["a"].running = true // keep tick from launching the fake command
	c.tick(context.Background(), now)
	if !c.state["a"].next.IsZero() {
		t.Fatal("a changed command should reset the schedule")
	}

	cfg = &config.Config{}
	c.tick(context.Background(), now)
	if len(c.state) != 0 || c.Snapshot(now).Present {
		t.Fatal("a source removed from the config should be dropped")
	}
}
