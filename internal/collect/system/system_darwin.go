package system

import (
	"time"

	"github.com/shirou/gopsutil/v4/mem"
)

// macOS: tool output matches what Activity Monitor and pmset report; no root needed.

func diskPath() string { return "/System/Volumes/Data" }

func (c *Collector) mem(iv time.Duration) error {
	vm, err := mem.VirtualMemory()
	if err != nil {
		return err
	}
	out, err := run("vm_stat")
	if err != nil {
		return err
	}
	m := ParseVMStat(out, float64(vm.Total))
	extra := M{"total": m.Total, "used": m.Used, "wired": m.Wired, "compressed": m.Compressed}
	if s, err := run("sysctl", "-n", "vm.swapusage"); err == nil {
		if b, ok := ParseSwapMB(s); ok {
			extra["swapBytes"] = b
		}
	}
	pct := 100 * m.Used / m.Total
	c.push("mem", pct)
	c.set("mem", metric(pct, iv, extra))
	return nil
}

func (c *Collector) procs(iv time.Duration) error {
	out, err := run("ps", "-Aceo", "pid,pcpu,rss,comm", "-r")
	if err != nil {
		return err
	}
	list := ParsePS(out)
	if len(list) > 6 {
		list = list[:6]
	}
	c.set("procs", metric(list, iv, nil))
	return nil
}

func (c *Collector) gpu(iv time.Duration) error {
	out, err := run("ioreg", "-r", "-d", "1", "-c", "IOAccelerator")
	if err != nil {
		return err
	}
	if v, ok := ParseGPU(out); ok {
		c.push("gpu", v)
		c.set("gpu", metric(v, iv, nil))
	} else {
		c.set("gpu", unavailable(iv, "not reported"))
	}
	return nil
}

func (c *Collector) battery(iv time.Duration) error {
	out, err := run("pmset", "-g", "batt")
	if err != nil {
		return err
	}
	if b := ParseBattery(out); b != nil {
		c.set("battery", metric(b.Percent, iv, M{"percent": b.Percent, "state": b.State, "source": b.Source, "remaining": b.Remaining}))
	} else {
		c.set("battery", unavailable(iv, "no battery"))
	}
	return nil
}

func (c *Collector) thermal(iv time.Duration) error {
	out, err := run("pmset", "-g", "therm")
	if err != nil {
		return err
	}
	if t := ParseThermal(out); t != "" {
		c.set("thermal", metric(t, iv, M{"note": "temperature sensors need root"}))
	} else {
		c.set("thermal", unavailable(iv, "not reported"))
	}
	return nil
}
