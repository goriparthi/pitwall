// Package bridge renders the panel view in headless Chromium (native orientation via CSS) and streams its
// screencast JPEGs straight to the screen. The UI stays a web page; this package only captures it.
package bridge

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"github.com/goriparthi/pitwall/internal/config"
	"github.com/goriparthi/pitwall/internal/display/us88"
	"github.com/goriparthi/pitwall/internal/logx"
)

type Host interface {
	SetDisplay(map[string]any)
	Brightness() int
}

type Bridge struct {
	Cfg  *config.Store
	Log  *logx.Logger
	Host Host
	Base string

	mu         sync.Mutex
	screen     *us88.Screen
	connecting atomic.Bool
	frames     atomic.Int64
	lastBytes  atomic.Int64
	brightness int
}

func (b *Bridge) report(state string, extra map[string]any) {
	m := map[string]any{"state": state}
	b.mu.Lock()
	if b.screen != nil {
		m["firmware"] = b.screen.Firmware
	}
	b.mu.Unlock()
	for k, v := range extra {
		m[k] = v
	}
	b.Host.SetDisplay(m)
}

// connect is the single reconnect path, with exponential backoff; frames are dropped while disconnected.
func (b *Bridge) connect(ctx context.Context, reason string, kick func()) {
	if !b.connecting.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer b.connecting.Store(false)
		b.mu.Lock()
		old := b.screen
		b.screen = nil
		b.mu.Unlock()
		if old != nil {
			_ = old.Close()
		}
		delay := 2 * time.Second
		for ctx.Err() == nil {
			s, err := us88.Open()
			if err == nil {
				b.mu.Lock()
				b.screen, b.brightness = s, 0
				b.mu.Unlock()
				b.Log.Info("screen connected", map[string]any{"firmware": s.Firmware, "after": reason})
				b.report("streaming", map[string]any{"detail": "connected"})
				kick()
				return
			}
			b.report("disconnected", map[string]any{"detail": err.Error()})
			select {
			case <-ctx.Done():
			case <-time.After(delay):
			}
			delay = min(delay*2, 30*time.Second)
		}
	}()
}

func (b *Bridge) push(ctx context.Context, frame []byte, kick func()) {
	if b.connecting.Load() {
		return
	}
	b.mu.Lock()
	s := b.screen
	b.mu.Unlock()
	if s == nil {
		return
	}
	if _, err := s.PushJPEG(frame); err != nil {
		b.Log.Warn("frame push failed; reconnecting", map[string]any{"message": err.Error()})
		b.report("reconnecting", map[string]any{"error": err.Error()})
		b.connect(ctx, err.Error(), kick)
		return
	}
	b.frames.Add(1)
	b.lastBytes.Store(int64(len(frame)))
}

func (b *Bridge) Run(ctx context.Context) error {
	cfg := b.Cfg.Get().Display
	exe := FindBrowser()
	if exe == "" {
		b.report("error", map[string]any{"error": "no Chrome, Edge or Brave found; set PITWALL_CHROME"})
		return errors.New("no Chromium-based browser found")
	}
	portrait := cfg.Rotation == 90 || cfg.Rotation == 270
	w, h := 1920, 480
	if portrait {
		w, h = 480, 1920
	}
	// Chrome starts before the panel is claimed and its WebUSB device detection stays off,
	// so the renderer never touches the screen's USB interface.
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(exe),
		chromedp.WindowSize(w, h),
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("force-color-profile", "srgb"),
		chromedp.Flag("disable-features", "Translate,WebUsbDeviceDetection"),
		chromedp.Flag("mute-audio", true),
	)
	actx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	defer cancelAlloc()
	cctx, cancelChrome := chromedp.NewContext(actx)
	defer cancelChrome()

	url := fmt.Sprintf("%s/?view=panel&rotate=%d&fps=%d", b.Base, cfg.Rotation, cfg.FPS)
	if err := chromedp.Run(cctx, chromedp.EmulateViewport(int64(w), int64(h)), chromedp.Navigate(url)); err != nil {
		b.report("error", map[string]any{"error": "renderer failed: " + err.Error()})
		return err
	}

	interval := time.Second / time.Duration(cfg.FPS)
	var pushing atomic.Bool
	var nextSlot atomic.Int64
	var lastSent atomic.Int64
	ack := func(sid int64, at time.Time) {
		go func() {
			time.Sleep(time.Until(at))
			_ = chromedp.Run(cctx, page.ScreencastFrameAck(sid))
		}()
	}
	kick := func() {
		go func() {
			var buf []byte
			if err := chromedp.Run(cctx, chromedp.ActionFunc(func(c context.Context) error {
				var err error
				buf, err = page.CaptureScreenshot().WithFormat(page.CaptureScreenshotFormatJpeg).WithQuality(int64(cfg.JPEGQuality)).Do(c)
				return err
			})); err == nil {
				b.push(ctx, buf, func() {})
			}
		}()
	}
	// Chrome keeps two screencast frames in flight, so a late ack alone does not cap the rate: frames that
	// arrive before the next slot (with a quarter-interval tolerance) are acked late and dropped.
	chromedp.ListenTarget(cctx, func(ev any) {
		f, ok := ev.(*page.EventScreencastFrame)
		if !ok {
			return
		}
		now := time.Now()
		slot := time.UnixMilli(nextSlot.Load())
		if pushing.Load() || now.Before(slot.Add(-interval/4)) {
			ack(f.SessionID, slot)
			return
		}
		pushing.Store(true)
		next := now.Add(interval)
		nextSlot.Store(next.UnixMilli())
		go func() {
			defer pushing.Store(false)
			if data, err := base64.StdEncoding.DecodeString(f.Data); err == nil {
				b.push(ctx, data, kick)
			}
			lastSent.Store(time.Now().UnixMilli())
			ack(f.SessionID, next)
		}()
	})

	b.connect(ctx, "startup", kick)
	if err := chromedp.Run(cctx, page.StartScreencast().WithFormat(page.ScreencastFormatJpeg).WithQuality(int64(cfg.JPEGQuality)).WithMaxWidth(int64(w)).WithMaxHeight(int64(h))); err != nil {
		return err
	}

	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	since := time.Now()
	for {
		select {
		case <-ctx.Done():
			_ = chromedp.Run(cctx, page.StopScreencast())
			b.mu.Lock()
			s := b.screen
			b.screen = nil
			b.mu.Unlock()
			if s != nil {
				_ = s.Close()
			}
			b.report("stopped", nil)
			return nil
		case <-t.C:
			secs := time.Since(since).Seconds()
			fps := float64(b.frames.Swap(0)) / secs
			since = time.Now()
			b.mu.Lock()
			s := b.screen
			b.mu.Unlock()
			if s != nil {
				b.report("streaming", map[string]any{"fps": float64(int(fps*10)) / 10, "frameBytes": b.lastBytes.Load(), "detail": fmt.Sprintf("%s %d°", cfg.Transport, cfg.Rotation)})
				b.mu.Lock()
				have := b.brightness
				b.mu.Unlock()
				if want := b.Host.Brightness(); want != have && !b.connecting.Load() {
					if err := s.SetBrightness(want); err == nil {
						b.mu.Lock()
						b.brightness = want
						b.mu.Unlock()
					}
				}
			}
			// the page may have lost its stream after a renderer hiccup; reload if nothing was sent for 30 s
			if time.Since(time.UnixMilli(lastSent.Load())) > 30*time.Second {
				_ = chromedp.Run(cctx, chromedp.Reload())
				lastSent.Store(time.Now().UnixMilli())
			}
		}
	}
}
