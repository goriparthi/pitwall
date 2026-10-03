// Package tray is the menu bar (macOS) / notification area (Windows) companion with global hotkeys.
// It calls the server in-process; it never runs anything the launcher config does not allow.
package tray

import (
	"bytes"
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/goriparthi/pitwall/internal/collect/ops"
	"github.com/goriparthi/pitwall/internal/collect/redline"

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
	Restart     func()
}

const agentSlots = 6

// Run blocks on the main thread until the tray exits.
func Run(o Options) {
	systray.Run(func() { onReady(o) }, func() {})
}

func Stop() { systray.Quit() }

// setIcon: on macOS the dial shows the screen (green streaming, red otherwise) and the title carries agent
// status; Windows has no title, so its brand icon keeps the agent status dot.
var (
	iconMu   sync.Mutex
	lastIcon []byte
)

func setIcon(screenUp bool, windowsIco []byte) {
	iconMu.Lock()
	defer iconMu.Unlock()
	icon := windowsIco
	if runtime.GOOS == "darwin" {
		icon = dialDisconnected
		if screenUp {
			icon = dialConnected
		}
	}
	if bytes.Equal(lastIcon, icon) {
		return
	}
	lastIcon = icon
	if runtime.GOOS == "darwin" {
		systray.SetIcon(icon)
	} else {
		systray.SetTemplateIcon(menuTemplate, icon)
	}
}

func onReady(o Options) {
	setIcon(false, icoCalm)
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
			order := o.Srv.PageOrder()
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

// add creates a menu item (top level when parent is nil) with a template glyph; "" means no icon.
func add(parent *systray.MenuItem, title, tip, icon string, checkbox bool) *systray.MenuItem {
	var mi *systray.MenuItem
	switch {
	case parent == nil && checkbox:
		mi = systray.AddMenuItemCheckbox(title, tip, false)
	case parent == nil:
		mi = systray.AddMenuItem(title, tip)
	case checkbox:
		mi = parent.AddSubMenuItemCheckbox(title, tip, false)
	default:
		mi = parent.AddSubMenuItem(title, tip)
	}
	if icon != "" {
		if t, r := menuGlyph(icon); t != nil {
			mi.SetTemplateIcon(t, r)
		}
	}
	return mi
}

// info is a read-only status row: disabled so it never looks clickable.
func info(icon string) *systray.MenuItem {
	mi := add(nil, "", "", icon, false)
	mi.Disable()
	mi.Hide()
	return mi
}

func setDot(mi *systray.MenuItem, status string) {
	if d, ok := dots[status]; ok {
		mi.SetIcon(d)
	}
}

// brightnessLevels are the menu's presets; a level set elsewhere checks the nearest one.
var brightnessLevels = []int{100, 80, 60, 40, 20, 10}

func nearestLevel(b int) int {
	best := brightnessLevels[0]
	for _, v := range brightnessLevels {
		if abs(v-b) < abs(best-b) {
			best = v
		}
	}
	return best
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

var kindIcon = map[string]string{"terminal": "terminal", "open-app": "app", "open-url": "link"}
var statusWord = map[string]string{"working": "Working", "waiting": "Waiting for you", "failed": "Failed", "completed": "Done", "idle": "Idle", "unknown": "Unknown"}

func ago(ms float64) string {
	if ms <= 0 {
		return ""
	}
	d := time.Since(time.UnixMilli(int64(ms))).Round(time.Minute)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

func buildMenu(ctx context.Context, o Options) {
	cfg := o.Cfg.Get()

	// Status: what is happening right now, all read-only rows.
	header := add(nil, "Pitwall", "", "", false)
	header.SetTemplateIcon(menuTemplate, icoCalm)
	header.Disable()
	summary := info("agents")
	agents := make([]*systray.MenuItem, agentSlots)
	for i := range agents {
		agents[i] = add(nil, "", "", "", false)
		agents[i].Disable()
		agents[i].Hide()
	}
	opsRow := info("")
	limitsRow := info("gauge")
	screenRow := info("display")

	// Launch: the configured actions, one icon per kind.
	systray.AddSeparator()
	for _, a := range o.Srv.Launcher.List() {
		id := a.ID
		title := a.Label
		if a.Hint != "" {
			title = a.Label + "  ·  " + a.Hint
		}
		mi := add(nil, title, a.Hint, kindIcon[a.Kind], false)
		click(ctx, mi, func() {
			if r := o.Srv.RunAction(id); !r.OK {
				o.Log.Warn("tray action failed", map[string]any{"action": id, "error": r.Error})
			}
		})
	}

	// View: what the panel shows.
	systray.AddSeparator()
	layoutsMenu := add(nil, "Layout", "Dashboard template", "layers", false)
	layoutItems := map[string]*systray.MenuItem{}
	numbered := map[string]int{}
	for i, id := range o.Srv.PageOrder() {
		numbered[id] = i + 1
	}
	for _, l := range o.Srv.Layouts() {
		title := l.Name
		if n := numbered[l.ID]; n > 0 {
			title = fmt.Sprintf("%s   %s%d", l.Name, HotkeyLabel, n)
		}
		id := l.ID
		mi := add(layoutsMenu, title, "", "", true)
		layoutItems[id] = mi
		click(ctx, mi, func() { o.Srv.PatchUI(map[string]any{"page": id, "focus": false}) })
	}
	projMenu := add(nil, "Project", "Filter agents and launcher target", "folder", false)
	projItems := map[string]*systray.MenuItem{"all": add(projMenu, "All projects", "", "", true)}
	click(ctx, projItems["all"], func() { o.Srv.PatchUI(map[string]any{"project": "all"}) })
	for _, p := range cfg.Projects {
		id := p.ID
		projItems[id] = add(projMenu, p.Name, p.Path, "", true)
		click(ctx, projItems[id], func() { o.Srv.PatchUI(map[string]any{"project": id}) })
	}
	brightMenu := add(nil, "Screen brightness", "Brightness of the Lian Li screen", "sun", false)
	brightItems := map[int]*systray.MenuItem{}
	for _, lvl := range brightnessLevels {
		v := lvl
		brightItems[v] = add(brightMenu, fmt.Sprintf("%d%%", v), "", "", true)
		click(ctx, brightItems[v], func() { o.Srv.PatchUI(map[string]any{"brightness": float64(v)}) })
	}
	focus := add(nil, "Focus mode   "+HotkeyLabel+"F", "Only working, waiting and failed agents", "target", true)
	privacy := add(nil, "Privacy mode   "+HotkeyLabel+"H", "Hide names, tasks and details on the panel", "eyeoff", true)
	rotate := add(nil, "Rotate layouts   "+HotkeyLabel+"R", "Cycle through the layouts", "cycle", true)
	click(ctx, focus, func() { o.Srv.PatchUI(map[string]any{"focus": !o.Srv.UI().Focus}) })
	click(ctx, privacy, func() { o.Srv.PatchUI(map[string]any{"privacy": !o.Srv.UI().Privacy}) })
	click(ctx, rotate, func() { o.Srv.PatchUI(map[string]any{"rotate": !o.Srv.UI().Rotate}) })

	// App: dashboard, settings, lifecycle.
	systray.AddSeparator()
	dash := add(nil, "Open full dashboard   "+HotkeyLabel+"D", "", "display", false)
	pal := add(nil, "Command palette   "+HotkeyLabel+"K", "", "search", false)
	cfgItem := add(nil, "Edit config…", o.Cfg.File(), "settings", false)
	click(ctx, dash, func() { o.OpenBrowser(o.Base + "/?view=full") })
	click(ctx, pal, func() { o.OpenBrowser(o.Base + "/?view=full&palette=1") })
	click(ctx, cfgItem, func() { o.OpenBrowser(o.Cfg.File()) })
	systray.AddSeparator()
	restart := add(nil, "Restart Pitwall", "Stop cleanly and start again; reconnects the screen and LED ring", "restart", false)
	quit := add(nil, "Quit Pitwall", "", "power", false)
	click(ctx, restart, func() {
		if o.Restart != nil {
			o.Restart()
		}
	})
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
		snap := o.Srv.Snapshot()
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
				title := fmt.Sprintf("%s  ·  %s", name, statusWord[st])
				if since, _ := a["since"].(float64); since > 0 && st != "idle" {
					title += "  ·  " + ago(since)
				}
				agents[i].SetTitle(title)
				setDot(agents[i], st)
				agents[i].Show()
			}
		}
		for i := len(list); i < agentSlots; i++ {
			agents[i].Hide()
		}

		mode := "Live"
		if o.Srv.Demo {
			mode = "Demo data"
		}
		header.SetTitle("Pitwall  ·  " + mode)
		if len(list) == 0 {
			summary.SetTitle("No Claude Code sessions")
		} else {
			parts := []string{}
			for _, k := range []string{"working", "waiting", "failed", "completed", "idle"} {
				if n := count[k]; n > 0 {
					parts = append(parts, fmt.Sprintf("%d %s", n, map[string]string{"working": "working", "waiting": "waiting", "failed": "failed", "completed": "done", "idle": "idle"}[k]))
				}
			}
			summary.SetTitle(strings.Join(parts, "  ·  "))
		}
		summary.Show()

		if opsSnap, ok := snap["ops"].(ops.Snapshot); ok && len(opsSnap.Sources) > 0 {
			src := opsSnap.Sources[0]
			st, word := src.Status, strings.ToUpper(src.Status)
			switch {
			case !src.HasReading || src.Failing:
				st, word = "unknown", "no reading"
			case src.Stale:
				st, word = "unknown", "stale"
			}
			label := src.Label
			if ui.Privacy {
				label = "Ops"
			}
			opsRow.SetTitle(fmt.Sprintf("%s  ·  %s", label, word))
			setDot(opsRow, st)
			opsRow.Show()
		} else {
			opsRow.Hide()
		}

		if l, ok := snap["limits"].(redline.Snapshot); ok && l.Available && len(l.Windows) > 0 {
			parts := []string{}
			for _, w := range l.Windows {
				parts = append(parts, fmt.Sprintf("%s %d%% used", w.Name, int(w.Used+0.5)))
			}
			limitsRow.SetTitle("Plan  ·  " + strings.Join(parts, "  ·  "))
			limitsRow.Show()
		} else {
			limitsRow.Hide()
		}

		screenUp := false
		if d, ok := snap["display"].(map[string]any); ok {
			state, _ := d["state"].(string)
			screenUp = state == "streaming"
			title := "Screen  ·  " + map[string]string{"streaming": "streaming", "unknown": "starting", "stopped": "stopped", "error": "error", "disconnected": "disconnected"}[state]
			if state == "streaming" {
				if fps, ok := d["fps"].(float64); ok {
					title += fmt.Sprintf("  ·  %.0f fps", fps)
				}
			} else if title == "Screen  ·  " {
				title += state
			}
			screenRow.SetTitle(title)
			screenRow.Show()
		} else {
			screenRow.Hide()
		}

		switch {
		case count["waiting"] > 0:
			setIcon(screenUp, icoWaiting)
			systray.SetTitle(fmt.Sprintf("▲ %d", count["waiting"]))
		case count["failed"] > 0:
			setIcon(screenUp, icoFailed)
			systray.SetTitle(fmt.Sprintf("✕ %d", count["failed"]))
		case count["working"] > 0:
			setIcon(screenUp, icoWorking)
			systray.SetTitle(fmt.Sprintf("%d", count["working"]))
		default:
			setIcon(screenUp, icoCalm)
			systray.SetTitle("")
		}
		screen := "screen not connected"
		if screenUp {
			screen = "screen connected"
		}
		systray.SetTooltip(fmt.Sprintf("Pitwall: %s · %d working, %d waiting", screen, count["working"], count["waiting"]))
		setCheck(focus, ui.Focus)
		setCheck(privacy, ui.Privacy)
		setCheck(rotate, ui.Rotate)
		for id, mi := range layoutItems {
			setCheck(mi, id == ui.Page && !ui.Focus)
		}
		for id, mi := range projItems {
			setCheck(mi, id == ui.Project)
		}
		brightMenu.SetTitle(fmt.Sprintf("Screen brightness  ·  %d%%", ui.Brightness))
		near := nearestLevel(ui.Brightness)
		for v, mi := range brightItems {
			setCheck(mi, v == near)
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
