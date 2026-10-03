package system

import (
	"regexp"
	"strconv"
	"strings"
)

// Parsers for macOS tool output; pure so they are testable on any OS.

type MemInfo struct{ Total, Used, Wired, Compressed float64 }

var (
	pageSizeRe = regexp.MustCompile(`page size of (\d+) bytes`)
	psRowRe    = regexp.MustCompile(`^\s*(\d+)\s+([\d.]+)\s+(\d+)\s+(.+)$`)
	battRe     = regexp.MustCompile(`(\d+)%;\s*([^;]+);\s*([^\n]*)`)
	remainRe   = regexp.MustCompile(`(\d+:\d+) remaining`)
	gpuRe      = regexp.MustCompile(`"Device Utilization %"=(\d+)`)
	speedRe    = regexp.MustCompile(`CPU_Speed_Limit\s*=\s*(\d+)`)
	thermLvlRe = regexp.MustCompile(`(?i)thermal warning level[^\d]*(\d+)`)
	swapRe     = regexp.MustCompile(`used = ([\d.]+)M`)
)

// ParseVMStat mirrors Activity Monitor's "Memory Used": anonymous - purgeable + wired + compressed.
func ParseVMStat(text string, total float64) MemInfo {
	ps := 16384.0
	if m := pageSizeRe.FindStringSubmatch(text); m != nil {
		ps, _ = strconv.ParseFloat(m[1], 64)
	}
	pages := func(label string) float64 {
		m := regexp.MustCompile(regexp.QuoteMeta(label) + `:\s+(\d+)`).FindStringSubmatch(text)
		if m == nil {
			return 0
		}
		v, _ := strconv.ParseFloat(m[1], 64)
		return v
	}
	app := pages("Anonymous pages") - pages("Pages purgeable")
	if app < 0 {
		app = 0
	}
	wired, comp := pages("Pages wired down"), pages("Pages occupied by compressor")
	used := (app + wired + comp) * ps
	if used > total {
		used = total
	}
	return MemInfo{Total: total, Used: used, Wired: wired * ps, Compressed: comp * ps}
}

func ParseSwapMB(text string) (float64, bool) {
	m := swapRe.FindStringSubmatch(text)
	if m == nil {
		return 0, false
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	return v * 1048576, true
}

type Proc struct {
	PID      int     `json:"pid"`
	CPU      float64 `json:"cpu"`
	RSSBytes float64 `json:"rssBytes"`
	Name     string  `json:"name"`
}

func ParsePS(text string) []Proc {
	var out []Proc
	lines := strings.Split(text, "\n")
	for _, l := range lines[min(1, len(lines)):] {
		m := psRowRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		pid, _ := strconv.Atoi(m[1])
		cpu, _ := strconv.ParseFloat(m[2], 64)
		rss, _ := strconv.ParseFloat(m[3], 64)
		out = append(out, Proc{PID: pid, CPU: cpu, RSSBytes: rss * 1024, Name: strings.TrimSpace(m[4])})
	}
	return out
}

type Battery struct {
	Percent   float64 `json:"percent"`
	State     string  `json:"state"`
	Source    string  `json:"source"`
	Remaining string  `json:"remaining,omitempty"`
}

func ParseBattery(text string) *Battery {
	m := battRe.FindStringSubmatch(text)
	if m == nil {
		return nil
	}
	p, _ := strconv.ParseFloat(m[1], 64)
	b := &Battery{Percent: p, State: strings.TrimSpace(m[2]), Source: "battery"}
	if strings.Contains(text, "AC Power") {
		b.Source = "ac"
	}
	if r := remainRe.FindStringSubmatch(m[3]); r != nil {
		b.Remaining = r[1]
	}
	return b
}

func ParseGPU(text string) (float64, bool) {
	m := gpuRe.FindStringSubmatch(text)
	if m == nil {
		return 0, false
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	return v, true
}

func ParseThermal(text string) string {
	if m := speedRe.FindStringSubmatch(text); m != nil {
		if v, _ := strconv.Atoi(m[1]); v < 100 {
			return "throttled"
		}
		return "nominal"
	}
	if strings.Contains(text, "No thermal warning level has been recorded") {
		return "nominal"
	}
	if m := thermLvlRe.FindStringSubmatch(text); m != nil {
		if v, _ := strconv.Atoi(m[1]); v > 0 {
			return "elevated"
		}
		return "nominal"
	}
	return ""
}

// SkipInterface excludes loopback, tunnels and Apple-internal links from throughput totals.
var skipIfaceRe = regexp.MustCompile(`^(lo|gif|stf|anpi|ap|awdl|llw|utun|bridge|Loopback|isatap|Teredo)`)

func SkipInterface(name string) bool { return skipIfaceRe.MatchString(name) }
