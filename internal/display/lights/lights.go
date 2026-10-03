// Package lights maps state to the LED ring: amber breathing while an agent waits, dim red on an agent failure
// or a critical ops reading, faint blue while working, otherwise off. The ring is turned off on exit so it never shows a stale alert.
package lights

import (
	"context"
	"math"
	"time"

	"github.com/goriparthi/pitwall/internal/config"
	"github.com/goriparthi/pitwall/internal/display/ledring"
	"github.com/goriparthi/pitwall/internal/logx"
)

type Signal struct {
	Mode  string // breathe | solid | off
	Color ledring.RGB
	Level float64
}

var (
	amber = ledring.RGB{255, 150, 0}
	red   = ledring.RGB{255, 20, 10}
	blue  = ledring.RGB{40, 120, 255}
)

// Inputs is everything the LED policy weighs.
type Inputs struct {
	Statuses    []string // agent statuses
	OpsCritical bool     // a fresh ops reading is critical
}

// Policy: waiting beats failed or ops critical beats working; calm is off.
func Policy(in Inputs, workingGlow bool) Signal {
	has := map[string]bool{}
	for _, s := range in.Statuses {
		has[s] = true
	}
	switch {
	case has["waiting"]:
		return Signal{Mode: "breathe", Color: amber}
	case has["failed"] || in.OpsCritical:
		return Signal{Mode: "solid", Color: red, Level: 0.4}
	case workingGlow && has["working"]:
		return Signal{Mode: "solid", Color: blue, Level: 0.12}
	}
	return Signal{Mode: "off"}
}

func scale(c ledring.RGB, k float64) ledring.RGB {
	return ledring.RGB{byte(float64(c[0]) * k), byte(float64(c[1]) * k), byte(float64(c[2]) * k)}
}

// Run drives the ring until ctx ends. inputs is polled at 1 Hz; animation runs at 20 Hz only while breathing.
func Run(ctx context.Context, cfg func() *config.Config, inputs func() Inputs, log *logx.Logger) {
	var ring *ledring.Ring
	sig := Signal{Mode: "off"}
	var last ledring.RGB
	var lastWrite, lastPoll, lastOpen time.Time
	defer func() {
		if ring != nil {
			_ = ring.Fill(ledring.RGB{})
			_ = ring.Close()
		}
	}()
	t := time.NewTicker(50 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			c := cfg()
			if !c.LED.Enabled {
				if ring != nil {
					_ = ring.Fill(ledring.RGB{})
					_ = ring.Close()
					ring = nil
				}
				continue
			}
			if ring == nil {
				if now.Sub(lastOpen) < 10*time.Second {
					continue
				}
				lastOpen = now
				r, err := ledring.Open()
				if err != nil {
					continue
				}
				ring, lastWrite = r, time.Time{}
				log.Info("LED ring connected")
			}
			if now.Sub(lastPoll) >= time.Second {
				lastPoll = now
				next := Policy(inputs(), c.LED.WorkingGlow)
				if next.Mode != sig.Mode || next.Color != sig.Color {
					log.Info("LED signal", map[string]any{"mode": next.Mode})
				}
				sig = next
			}
			max := float64(c.LED.MaxBrightness) / 100
			var rgb ledring.RGB
			switch sig.Mode {
			case "breathe":
				k := 0.15 + 0.85*(0.5+0.5*math.Sin(float64(now.UnixMilli())/1600*2*math.Pi))
				rgb = scale(sig.Color, k*max)
			case "solid":
				rgb = scale(sig.Color, sig.Level*max)
			}
			// write every frame while breathing; otherwise on change plus a 10 s refresh after replugs
			if sig.Mode == "breathe" || rgb != last || now.Sub(lastWrite) > 10*time.Second {
				if err := ring.Fill(rgb); err != nil {
					log.Warn("LED write failed; reopening", map[string]any{"message": err.Error()})
					_ = ring.Close()
					ring = nil
					continue
				}
				last, lastWrite = rgb, now
			}
		}
	}
}
