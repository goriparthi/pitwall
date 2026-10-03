package redline

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseStatusWithPace(t *testing.T) {
	s, err := ParseStatus([]byte(`{"claude_limits_as_of":"2026-10-03T08:35:34Z","today":{"cost_basis":"local_estimate","cost_usd":251.05,"tokens":354151,"tokens_basis":"official"},
	 "week":{"cost_usd":489.6,"tokens":744068},
	 "windows":[{"key":"five_hour","provider":"Claude","resets_at":"2026-10-03T08:50:00Z","utilization":35,"provenance":"official",
	   "pace":{"basis":"measured from 11 readings over 35m","exhausts_at":"2026-10-03T13:21:02Z","hits_limit_before_reset":false,"rate_per_hour":13.66}},
	  {"key":"seven_day","provider":"Claude","resets_at":"2026-10-06T07:00:00Z","utilization":12}]}`))
	if err != nil || !s.Available || len(s.Windows) != 2 {
		t.Fatalf("%v %+v", err, s)
	}
	w := s.Windows[0]
	if w.Name != "Session" || w.Used != 35 || w.LengthMs != 5*3600*1000 || w.Pace == nil || w.Pace.RatePerHour != 13.66 || w.Pace.ExhaustsAt == 0 {
		t.Fatalf("%+v %+v", w, w.Pace)
	}
	if s.Windows[1].Name != "Week" || s.Windows[1].Pace != nil || s.Today.CostBasis != "local_estimate" || s.Week.Tokens != 744068 {
		t.Fatalf("%+v", s)
	}
}

func TestParseUsageSnapshotFallsBackToStandardKeys(t *testing.T) {
	s, err := ParseUsageSnapshot([]byte(`{"updated_at":"2026-10-03T08:30:26Z","five_hour":{"resets_at":"2026-10-03T08:50:00Z","used_percentage":35},"seven_day":{"resets_at":"2026-10-06T07:00:00Z","utilization":12}}`))
	if err != nil || len(s.Windows) != 2 || s.Windows[1].Used != 12 || s.Windows[0].ResetsAt == 0 {
		t.Fatalf("%v %+v", err, s)
	}
}

func TestParseFeedEpochSeconds(t *testing.T) {
	s, err := ParseFeed([]byte(`{"updated_at":"2026-10-03T08:35:09Z","five_hour":{"used_percentage":35,"resets_at":1791017400},"seven_day":null}`))
	if err != nil || len(s.Windows) != 1 || s.Windows[0].ResetsAt != 1791017400000 || s.Source != "statusline feed" {
		t.Fatalf("%v %+v", err, s)
	}
}

func TestReadMarksInstalledFromFeed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PITWALL_REDLINE_DIR", dir)
	t.Setenv("PITWALL_REDLINE_CLI", "/nonexistent/redline")
	t.Setenv("PATH", "")
	if err := os.WriteFile(filepath.Join(dir, "claude-usage.json"), []byte(`{"updated_at":"2026-10-03T08:35:09Z","five_hour":{"used_percentage":35,"resets_at":1791017400}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &Collector{}
	c.read()
	if s := c.Snapshot(); !s.Installed || !s.Available || len(s.Windows) != 1 {
		t.Fatalf("%+v", s)
	}
}

func TestReadNotInstalled(t *testing.T) {
	t.Setenv("PITWALL_REDLINE_DIR", t.TempDir())
	t.Setenv("PITWALL_REDLINE_CLI", "/nonexistent/redline")
	t.Setenv("PATH", "")
	t.Setenv("HOME", t.TempDir())
	c := &Collector{}
	c.read()
	if s := c.Snapshot(); s.Installed || s.Available {
		t.Fatalf("%+v", s)
	}
}
