package system

import (
	"os"
	"sort"
	"strings"
	"time"
	"unsafe"

	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
	"golang.org/x/sys/windows"
)

// Windows: gopsutil for memory and processes, kernel32 for battery. GPU and thermal are not collected yet.

func diskPath() string {
	if d := os.Getenv("SystemDrive"); d != "" {
		return d + `\`
	}
	return `C:\`
}

func (c *Collector) mem(iv time.Duration) error {
	vm, err := mem.VirtualMemory()
	if err != nil {
		return err
	}
	extra := M{"total": float64(vm.Total), "used": float64(vm.Used)}
	if sw, err := mem.SwapMemory(); err == nil {
		extra["swapBytes"] = float64(sw.Used)
	}
	c.push("mem", vm.UsedPercent)
	c.set("mem", metric(vm.UsedPercent, iv, extra))
	return nil
}

// Per-process CPU needs two samples, so process handles are kept between runs.
var procCache = map[int32]*process.Process{}

func (c *Collector) procs(iv time.Duration) error {
	list, err := process.Processes()
	if err != nil {
		return err
	}
	seen := map[int32]bool{}
	var out []Proc
	for _, p := range list {
		seen[p.Pid] = true
		cached, ok := procCache[p.Pid]
		if !ok {
			procCache[p.Pid] = p
			_, _ = p.Percent(0)
			continue
		}
		pct, err := cached.Percent(0)
		if err != nil {
			continue
		}
		name, _ := cached.Name()
		var rss float64
		if mi, err := cached.MemoryInfo(); err == nil {
			rss = float64(mi.RSS)
		}
		out = append(out, Proc{PID: int(p.Pid), CPU: pct, RSSBytes: rss, Name: strings.TrimSuffix(name, ".exe")})
	}
	for pid := range procCache {
		if !seen[pid] {
			delete(procCache, pid)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CPU > out[j].CPU })
	if len(out) > 6 {
		out = out[:6]
	}
	c.set("procs", metric(out, iv, nil))
	return nil
}

func (c *Collector) gpu(iv time.Duration) error {
	c.set("gpu", unavailable(iv, "not collected on Windows yet"))
	return nil
}

type systemPowerStatus struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

var procGetSystemPowerStatus = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetSystemPowerStatus")

func (c *Collector) battery(iv time.Duration) error {
	var s systemPowerStatus
	if r, _, err := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&s))); r == 0 {
		return err
	}
	if s.BatteryFlag == 128 || s.BatteryLifePercent == 255 {
		c.set("battery", unavailable(iv, "no battery"))
		return nil
	}
	src, state := "battery", "discharging"
	if s.ACLineStatus == 1 {
		src, state = "ac", "charging"
		if s.BatteryLifePercent >= 100 {
			state = "charged"
		}
	}
	b := M{"percent": float64(s.BatteryLifePercent), "state": state, "source": src}
	if s.BatteryLifeTime != 0xFFFFFFFF && src == "battery" {
		b["remaining"] = (time.Duration(s.BatteryLifeTime) * time.Second).Truncate(time.Minute).String()
	}
	c.set("battery", metric(float64(s.BatteryLifePercent), iv, b))
	return nil
}

func (c *Collector) thermal(iv time.Duration) error {
	c.set("thermal", unavailable(iv, "not collected on Windows yet"))
	return nil
}
