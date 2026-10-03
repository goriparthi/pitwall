// Package tray is the menu bar (macOS) / notification area (Windows) companion with global hotkeys.
// It calls the server in-process; it never runs anything the launcher config does not allow.
package tray

import (
	"context"
	"fmt"
	"strings"
	"time"

	"fyne.io/systray"

	"github.com/goriparthi/pitwall/internal/config"
	"github.com/goriparthi/pitwall/internal/logx"
	"github.com/goriparthi/pitwall/internal/server"
)

type Options struct {
	Srv         *server.Server
	Cfg         *config.Store
	Log         *logx.Logger
	OpenBrowser func(url string)
	Base        string
	Quit        func()
}

const agentSlots = 6

var glyph = map[string]string{"waiting": "▲", "failed": "✕", "working": "●", "completed": "✓", "idle": "○", "unknown": "○"}

// Run blocks on the main thread until the tray exits.
func Run(o Options) {
	systray.Run(func() { onReady(o) }, func() {})
}

func Stop() { systray.Quit() }

func onReady(o Options) {
	systray.SetIcon(iconCalm)
	systray.SetTooltip("Pitwall")
	bind := map[Key]func(){
		'F': func() { o.Srv.PatchUI(map[string]any{"focus": !o.Srv.UI().Focus}) },
		'H': func() { o.Srv.PatchUI(map[string]any{"privacy": !o.Srv.UI().Privacy}) },
		'R': func() { o.Srv.PatchUI(map[string]any{"rotate": !o.Srv.UI().Rotate}) },
		'D': func() { o.OpenBrowser(o.Base + "/?view=full") },
		'K': func() { o.OpenBrowser(o.Base + "/?view=full&palette=1") },
	}
	for i := 0; i < 9; i++ {
		idx := i
		bind[Key('1'+i)] = func() {
			order := o.Cfg.Get().PageOrder()
			if idx < len(order) {
				o.Srv.PatchUI(map[string]any{"page": order[idx], "focus": false})
			}
		}
	}
	if failed := registerHotkeys(bind); len(failed) > 0 {
		o.Log.Warn("some global hotkeys are taken by other apps", map[string]any{"keys": strings.Join(failed, " ")})
	}

	var cancel context.CancelFunc = func() {}
	rebuild := func() {
		cancel()
		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())
		systray.ResetMenu()
		buildMenu(ctx, o)
	}
	rebuild()
	o.Cfg.OnLoad = func() { rebuild() }
}

func click(ctx context.Context, mi *systray.MenuItem, fn func()) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-mi.ClickedCh:
				fn()
			}
		}
	}()
}

func buildMenu(ctx context.Context, o Options) {
	cfg := o.Cfg.Get()
	header := systray.AddMenuItem("Pitwall", "")
	header.Disable()
	agents := make([]*systray.MenuItem, agentSlots)
	for i := range agents {
		agents[i] = systray.AddMenuItem("", "")
		agents[i].Disable()
		agents[i].Hide()
	}
	systray.AddSeparator()
	for _, a := range o.Srv.Launcher.List() {
		id := a.ID
		mi := systray.AddMenuItem(a.Label, a.Hint)
		click(ctx, mi, func() {
			if r := o.Srv.RunAction(id); !r.OK {
				o.Log.Warn("tray action failed", map[string]any{"action": id, "error": r.Error})
			}
		})
	}
	systray.AddSeparator()
	focus := systray.AddMenuItemCheckbox("Focus mode   "+HotkeyLabel+"F", "", false)
	privacy := systray.AddMenuItemCheckbox("Privacy mode   "+HotkeyLabel+"H", "", false)
	rotate := systray.AddMenuItemCheckbox("Rotate layouts   "+HotkeyLabel+"R", "", false)
	click(ctx, focus, func() { o.Srv.PatchUI(map[string]any{"focus": !o.Srv.UI().Focus}) })
	click(ctx, privacy, func() { o.Srv.PatchUI(map[string]any{"privacy": !o.Srv.UI().Privacy}) })
	click(ctx, rotate, func() { o.Srv.PatchUI(map[string]any{"rotate": !o.Srv.UI().Rotate}) })

	layoutsMenu := systray.AddMenuItem("Layout", "Dashboard template")
	layoutItems := map[string]*systray.MenuItem{}
	numbered := map[string]int{}
	for i, id := range cfg.PageOrder() {
		numbered[id] = i + 1
	}
	for _, l := range cfg.AllLayouts() {
		title := l.Name
		if n := numbered[l.ID]; n > 0 {
			title = fmt.Sprintf("%s   %s%d", l.Name, HotkeyLabel, n)
		}
		id := l.ID
		mi := layoutsMenu.AddSubMenuItemCheckbox(title, "", false)
		layoutItems[id] = mi
		click(ctx, mi, func() { o.Srv.PatchUI(map[string]any{"page": id, "focus": false}) })
	}
	projMenu := systray.AddMenuItem("Project", "Filter agents and launcher target")
	projItems := map[string]*systray.MenuItem{"all": projMenu.AddSubMenuItemCheckbox("All projects", "", false)}
	click(ctx, projItems["all"], func() { o.Srv.PatchUI(map[string]any{"project": "all"}) })
	for _, p := range cfg.Projects {
		id := p.ID
		projItems[id] = projMenu.AddSubMenuItemCheckbox(p.Name, p.Path, false)
		click(ctx, projItems[id], func() { o.Srv.PatchUI(map[string]any{"project": id}) })
	}
	systray.AddSeparator()
	dash := systray.AddMenuItem("Open full dashboard   "+HotkeyLabel+"D", "")
	pal := systray.AddMenuItem("Command palette   "+HotkeyLabel+"K", "")
	cfgItem := systray.AddMenuItem("Edit config…", o.Cfg.File())
	quit := systray.AddMenuItem("Quit Pitwall", "")
	click(ctx, dash, func() { o.OpenBrowser(o.Base + "/?view=full") })
	click(ctx, pal, func() { o.OpenBrowser(o.Base + "/?view=full&palette=1") })
	click(ctx, cfgItem, func() { o.OpenBrowser(o.Cfg.File()) })
	click(ctx, quit, func() { o.Quit() })

	setCheck := func(mi *systray.MenuItem, on bool) {
		if on {
			mi.Check()
		} else {
			mi.Uncheck()
		}
	}
	update := func() {
		ui := o.Srv.UI()
		list := o.Srv.Agents()
		count := map[string]int{}
		for i, a := range list {
			st, _ := a["status"].(string)
			count[st]++
			if i < agentSlots {
				name, _ := a["name"].(string)
				if name == "" {
					name, _ = a["project"].(string)
				}
				if ui.Privacy || name == "" {
					name = fmt.Sprintf("Session %d", i+1)
				}
				agents[i].SetTitle(fmt.Sprintf("%s  %s  ·  %s", glyph[st], name, st))
				agents[i].Show()
			}
		}
		for i := len(list); i < agentSlots; i++ {
			agents[i].Hide()
		}
		mode := "live"
		if o.Srv.Demo {
			mode = "demo data"
		}
		header.SetTitle(fmt.Sprintf("Pitwall · %s · %d session(s)", mode, len(list)))
		switch {
		case count["waiting"] > 0:
			systray.SetIcon(iconWaiting)
			systray.SetTitle(fmt.Sprintf("▲ %d", count["waiting"]))
		case count["failed"] > 0:
			systray.SetIcon(iconFailed)
			systray.SetTitle(fmt.Sprintf("✕ %d", count["failed"]))
		case count["working"] > 0:
			systray.SetIcon(iconWorking)
			systray.SetTitle(fmt.Sprintf("%d", count["working"]))
		default:
			systray.SetIcon(iconCalm)
			systray.SetTitle("")
		}
		systray.SetTooltip(fmt.Sprintf("Pitwall: %d working, %d waiting", count["working"], count["waiting"]))
		setCheck(focus, ui.Focus)
		setCheck(privacy, ui.Privacy)
		setCheck(rotate, ui.Rotate)
		for id, mi := range layoutItems {
			setCheck(mi, id == ui.Page && !ui.Focus)
		}
		for id, mi := range projItems {
			setCheck(mi, id == ui.Project)
		}
	}
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			update()
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}
