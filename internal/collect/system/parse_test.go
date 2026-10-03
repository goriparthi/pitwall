package system

import "testing"

func TestVMStat(t *testing.T) {
	text := `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages wired down:                        236994.
Pages purgeable:                          15320.
Anonymous pages:                         754965.
Pages occupied by compressor:            925315.`
	m := ParseVMStat(text, 36*(1<<30))
	if want := float64(754965-15320+236994+925315) * 16384; m.Used != want {
		t.Fatalf("used %v want %v", m.Used, want)
	}
}

func TestPSBatteryGPUThermal(t *testing.T) {
	ps := ParsePS("  PID  %CPU    RSS COMM\n98007  99.9  11312 BTLEServer\n19709  45.5 132528 Window Server\n")
	if len(ps) != 2 || ps[1].Name != "Window Server" || ps[1].RSSBytes != 132528*1024 {
		t.Fatalf("ps %+v", ps)
	}
	b := ParseBattery("Now drawing from 'Battery Power'\n -InternalBattery-0 (id=1)\t100%; discharging; 10:39 remaining present: true")
	if b == nil || b.Percent != 100 || b.Source != "battery" || b.Remaining != "10:39" {
		t.Fatalf("battery %+v", b)
	}
	if ParseBattery("Now drawing from 'AC Power'\n") != nil {
		t.Fatal("desktop Mac should have no battery")
	}
	if v, ok := ParseGPU(`"Device Utilization %"=16,"Renderer Utilization %"=10`); !ok || v != 16 {
		t.Fatal("gpu")
	}
	for in, want := range map[string]string{
		"Note: No thermal warning level has been recorded": "nominal",
		"CPU_Speed_Limit = 70":                             "throttled",
		"Thermal warning level set to 2.":                  "elevated",
	} {
		if got := ParseThermal(in); got != want {
			t.Errorf("%q: %s", in, got)
		}
	}
	if !SkipInterface("utun3") || !SkipInterface("lo0") || SkipInterface("en0") {
		t.Fatal("interface filter")
	}
}
