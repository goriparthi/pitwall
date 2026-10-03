package ledring

import "testing"

func TestPackets(t *testing.T) {
	colors := make([]RGB, Count)
	for i := range colors {
		colors[i] = RGB{byte(i), 100, 200}
	}
	p := Packets(colors)
	if len(p) != 3 {
		t.Fatalf("want 3 packets, got %d", len(p))
	}
	for i, pk := range p {
		if len(pk) != 64 || pk[0] != 0x11 || pk[1] != byte(i*20) {
			t.Fatalf("packet %d header % x", i, pk[:4])
		}
	}
	if p[1][4] != 20 || p[2][61] != 59 || p[2][63] != 200 {
		t.Fatal("RGB placement wrong")
	}
}
