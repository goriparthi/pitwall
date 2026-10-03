// Package system collects host metrics on a per-metric cadence. Shared logic here; OS specifics in system_<os>.go.
package system

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/load"
	gnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"

	"github.com/goriparthi/pitwall/internal/logx"
)

const historyLen = 90

type M = map[string]any

func metric(value any, interval time.Duration, extra M) M {
	m := M{"value": value, "updatedAt": time.Now().UnixMilli(), "intervalMs": interval.Milliseconds()}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func unavailable(interval time.Duration, reason string) M {
	return M{"value": nil, "updatedAt": time.Now().UnixMilli(), "intervalMs": interval.Milliseconds(), "unavailable": reason}
}

type Collector struct {
	log     *logx.Logger
	mu      sync.RWMutex
	state   M
	history map[string][]float64
	prevNet struct {
		rx, tx float64
		t      time.Time
	}
	self *process.Process
}

func New(log *logx.Logger) *Collector {
	host, _ := os.Hostname()
	host = strings.TrimSuffix(host, ".local")
	model := ""
	if info, err := cpu.Info(); err == nil && len(info) > 0 {
		model = info[0].ModelName
	}
	self, _ := process.NewProcess(int32(os.Getpid()))
	return &Collector{
		log:     log,
		state:   M{"host": host, "cpuModel": model, "cores": runtime.NumCPU()},
		history: map[string][]float64{"cpu": {}, "mem": {}, "netRx": {}, "netTx": {}, "gpu": {}},
		self:    self,
	}
}

func (c *Collector) set(key string, v M) {
	c.mu.Lock()
	c.state[key] = v
	c.mu.Unlock()
}

func (c *Collector) push(key string, v float64) {
	c.mu.Lock()
	h := append(c.history[key], v)
	if len(h) > historyLen {
		h = h[len(h)-historyLen:]
	}
	c.history[key] = h
	c.mu.Unlock()
}

func (c *Collector) Snapshot() M {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(M, len(c.state)+1)
	for k, v := range c.state {
		out[k] = v
	}
	h := make(map[string][]float64, len(c.history))
	for k, v := range c.history {
		h[k] = append([]float64(nil), v...)
	}
	out["history"] = h
	return out
}

type task struct {
	every time.Duration
	name  string
	run   func(time.Duration) error
}

func (c *Collector) Start(ctx context.Context) {
	tasks := []task{
		{time.Second, "cpu", c.cpu},
		{2 * time.Second, "mem", c.mem},
		{2 * time.Second, "net", c.net},
		{15 * time.Second, "disk", c.disk},
		{4 * time.Second, "procs", c.procs},
		{3 * time.Second, "gpu", c.gpu},
		{30 * time.Second, "battery", c.battery},
		{30 * time.Second, "thermal", c.thermal},
		{5 * time.Second, "self", c.selfUsage},
	}
	for _, t := range tasks {
		go func(t task) {
			tick := time.NewTicker(t.every)
			defer tick.Stop()
			for {
				func() {
					defer func() {
						if r := recover(); r != nil {
							c.log.Error("system collector panic", M{"metric": t.name, "panic": r})
						}
					}()
					if err := t.run(t.every); err != nil {
						c.set(t.name, unavailable(t.every, "collection failed"))
						c.log.Warn("system."+t.name+" failed", M{"message": firstLine(err.Error())})
					}
				}()
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
				}
			}
		}(t)
	}
}

func (c *Collector) cpu(iv time.Duration) error {
	per, err := cpu.Percent(0, true)
	if err != nil {
		return err
	}
	if len(per) == 0 {
		return nil // first call establishes the baseline
	}
	sum := 0.0
	for _, v := range per {
		sum += v
	}
	avg := sum / float64(len(per))
	c.push("cpu", avg)
	extra := M{"perCore": per}
	if l, err := load.Avg(); err == nil && runtime.GOOS != "windows" {
		extra["load"] = []float64{l.Load1, l.Load5, l.Load15}
	}
	c.set("cpu", metric(avg, iv, extra))
	return nil
}

func (c *Collector) net(iv time.Duration) error {
	stats, err := gnet.IOCounters(true)
	if err != nil {
		return err
	}
	var rx, tx float64
	for _, s := range stats {
		if SkipInterface(strings.TrimSuffix(s.Name, "*")) {
			continue
		}
		rx += float64(s.BytesRecv)
		tx += float64(s.BytesSent)
	}
	now := time.Now()
	p := c.prevNet
	if !p.t.IsZero() && rx >= p.rx && tx >= p.tx {
		dt := now.Sub(p.t).Seconds()
		r, t := (rx-p.rx)/dt, (tx-p.tx)/dt
		c.push("netRx", r)
		c.push("netTx", t)
		c.set("net", metric(r+t, iv, M{"rxBps": r, "txBps": t}))
	}
	c.prevNet.rx, c.prevNet.tx, c.prevNet.t = rx, tx, now
	return nil
}

func (c *Collector) disk(iv time.Duration) error {
	u, err := disk.Usage(diskPath())
	if err != nil {
		return err
	}
	free, total := float64(u.Free), float64(u.Total)
	c.set("disk", metric(100*(total-free)/total, iv, M{"freeBytes": free, "totalBytes": total}))
	return nil
}

func (c *Collector) selfUsage(iv time.Duration) error {
	if c.self == nil {
		return nil
	}
	pct, err := c.self.Percent(0)
	if err != nil {
		return err
	}
	extra := M{}
	if mi, err := c.self.MemoryInfo(); err == nil {
		extra["rssBytes"] = mi.RSS
	}
	c.set("self", metric(pct, iv, extra))
	return nil
}

func run(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return string(out), err
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
