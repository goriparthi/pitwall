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
		if got := Policy(c.in, c.glow).Mode; got != c.mode {
			t.Errorf("%v glow=%v: got %s want %s", c.in, c.glow, got, c.mode)
		}
	}
	if Policy([]string{"failed"}, true).Color != red {
		t.Error("failure should be red")
	}
}
