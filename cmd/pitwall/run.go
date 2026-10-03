package main

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/goriparthi/pitwall/internal/collect/claude"
	"github.com/goriparthi/pitwall/internal/collect/demo"
	"github.com/goriparthi/pitwall/internal/collect/ops"
	"github.com/goriparthi/pitwall/internal/collect/redline"
	"github.com/goriparthi/pitwall/internal/collect/system"
	"github.com/goriparthi/pitwall/internal/collect/tools"
	"github.com/goriparthi/pitwall/internal/config"
	"github.com/goriparthi/pitwall/internal/display/bridge"
	"github.com/goriparthi/pitwall/internal/display/lights"
	"github.com/goriparthi/pitwall/internal/launcher"
	"github.com/goriparthi/pitwall/internal/logx"
	"github.com/goriparthi/pitwall/internal/server"
	"github.com/goriparthi/pitwall/internal/tray"
)

func runCmd(args []string) int {
	f := parseRun(args)
	startPprof()
	log := logx.New("pitwall")
	store, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		log.Error("config", map[string]any{"message": err.Error()})
		return 1
	}
	store.OnError = func(err error) {
		log.Error("config reload rejected; keeping previous config", map[string]any{"message": err.Error()})
	}

	ctx, stop := signalContext()
	defer stop()
	srv := &server.Server{Cfg: store, Log: log, Demo: f.demo, Launcher: launcher.New(store.Get)}
	srv.Launcher.Log = func(msg string, kv map[string]any) { log.Info(msg, kv) }
	if f.demo {
		srv.DemoSrc = demo.New()
	} else {
		srv.System = system.New(log)
		srv.Claude = claude.New(log, func() int { return store.Get().UI.CompletedHoldMinutes })
		srv.Tools = &tools.Collector{}
		if store.Get().Integrations.Redline != "off" {
			srv.Redline = &redline.Collector{}
			srv.Redline.Start(ctx)
		}
		// always running: sources can be added by editing the config without a restart
		srv.Ops = &ops.Collector{Cfg: store.Get, Log: log}
		srv.Ops.Start(ctx)
		srv.System.Start(ctx)
		srv.Claude.Start(ctx)
		srv.Tools.Start(ctx)
	}
	if err := srv.Init(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := srv.Start(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		log.Error("start", map[string]any{"message": err.Error()})
		return 1
	}
	stopWatch := make(chan struct{})
	go store.Watch(stopWatch)
	base := fmt.Sprintf("http://127.0.0.1:%d", store.Get().Server.Port)

	var wg sync.WaitGroup
	cfg := store.Get()
	if cfg.Display.Enabled && !f.noDisplay {
		dlog := logx.New("display")
		wg.Add(2)
		go func() {
			defer wg.Done()
			b := &bridge.Bridge{Cfg: store, Log: dlog, Host: srv, Base: base}
			for ctx.Err() == nil {
				if err := b.Run(ctx); err != nil {
					dlog.Error("bridge stopped; restarting in 10 s", map[string]any{"message": err.Error()})
					select {
					case <-ctx.Done():
					case <-time.After(10 * time.Second):
					}
				}
			}
		}()
		go func() {
			defer wg.Done()
			lights.Run(ctx, store.Get, srv.LightInputs, dlog)
		}()
	}

	var once sync.Once
	shutdown := func() {
		once.Do(func() {
			log.Info("shutting down")
			stop()
		})
	}
	srv.Shutdown = shutdown
	var restart atomic.Bool
	restartFn := func() {
		log.Info("restart requested")
		restart.Store(true)
		shutdown()
	}

	if f.noTray {
		<-ctx.Done()
	} else {
		go func() {
			<-ctx.Done()
			tray.Stop()
		}()
		tray.Run(tray.Options{Srv: srv, Cfg: store, Log: log, OpenBrowser: openBrowser, Base: base, Quit: shutdown, Restart: restartFn})
		shutdown()
	}
	close(stopWatch)
	// let the bridge finish its in-flight frame and the ring turn off before the process exits
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(6 * time.Second):
		log.Warn("display shutdown timed out")
	}
	srv.Stop()
	if restart.Load() {
		// the port, screen and LED ring are released by now, so the new process can claim them
		if err := relaunch(args); err != nil {
			log.Error("restart failed; start Pitwall again by hand", map[string]any{"message": err.Error()})
			return 1
		}
	}
	return 0
}
