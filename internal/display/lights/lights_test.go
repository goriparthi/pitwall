package lights

import "testing"

func TestPolicyPriority(t *testing.T) {
	cases := []struct {
		in   []string
		glow bool
		mode string
	}{
		{[]string{"working", "waiting", "failed"}, true, "breathe"},
		{[]string{"working", "failed"}, true, "solid"},
		{[]string{"working"}, true, "solid"},
		{[]string{"working"}, false, "off"},
		{[]string{"idle", "completed"}, true, "off"},
		{nil, true, "off"},
	}
	for _, c := range cases {
		if got := Policy(Inputs{Statuses: c.in}, c.glow).Mode; got != c.mode {
			t.Errorf("%v glow=%v: got %s want %s", c.in, c.glow, got, c.mode)
		}
	}
	if Policy(Inputs{Statuses: []string{"failed"}}, true).Color != red {
		t.Error("failure should be red")
	}
	if got := Policy(Inputs{Statuses: []string{"working"}, OpsCritical: true}, true); got.Color != red || got.Mode != "solid" {
		t.Errorf("ops critical should be solid red over the working glow, got %+v", got)
	}
	if got := Policy(Inputs{Statuses: []string{"waiting"}, OpsCritical: true}, true); got.Mode != "breathe" {
		t.Errorf("a waiting agent should beat ops critical, got %+v", got)
	}
}
