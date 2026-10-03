# Pitwall

A glanceable cockpit for the Lian Li 8.8" Universal Screen (US88, SM088X, 1920 x 480). It shows Claude Code agents with an attention queue, live machine health, and a launcher for configured actions. One Go binary for macOS and Windows. Everything stays on your machine.

## Quick start

```sh
scripts/build.sh                      # bin/pitwall for this Mac, dist/ for macOS arm64 + Windows amd64/arm64
bin/pitwall start             # background: server, panel, LED ring, menu bar item, hotkeys
bin/pitwall hooks install     # Claude Code hooks (backs up ~/.claude/settings.json)
bin/pitwall open              # full dashboard on the main monitor
bin/pitwall status | stop
bin/pitwall run --demo --no-display   # labelled demo data in the browser only
bin/pitwall selftest          # fake "needs approval" session: amber banner + LED breathing
go test ./...                         # macOS needs PKG_CONFIG=$PWD/scripts/pkg-config (scripts/build.sh sets it)
```

Requirements: Go 1.26+. On macOS, Homebrew `libusb` at build time (it is linked statically; the binary needs nothing at runtime) and Chrome, Edge or Brave for rendering. Windows builds need no cgo and render with Edge, which ships with Windows.

## Using it

- **Panel:** the Lian Li screen shows the active layout. It has no touch, so you drive it from the menu bar or global hotkeys.
- **Global hotkeys** (⌃⌥ on macOS, Ctrl+Alt on Windows): `F` focus, `H` privacy, `R` rotate layouts, `1`-`9` layouts, `D` dashboard, `K` command palette.
- **Full dashboard** (`open`, or `D`): the same view on your main monitor with controls, the attention queue, a brightness slider and ⌘K palette. Its keys work without the modifier.
- **Attention:** a waiting agent turns the panel banner and edge amber, makes the LED ring breathe amber, and shows `▲ n` in the menu bar. Failures are red; a working agent is a faint blue on the ring.

## Templates and customization

Built-in layouts: `balanced` (default), `agents`, `focus`, `usage`, `system`, `monitor`. Each is a grid of up to four widget slots: `ai`, `health`, `health-mini`, `launcher`, `usage`, `system`.

Edit the config (`pitwall config` prints its path; the menu has "Edit config…"). Changes apply within 2 seconds; invalid edits are rejected and logged, and the previous config stays active.

```json
"ui": { "layouts": ["balanced", "mine", "usage"] },
"layouts": [
  { "id": "mine", "name": "Wide agents", "columns": ["1300px", "1fr"], "slots": ["ai", "launcher"] },
  { "id": "balanced", "name": "Balanced", "columns": ["900px", "570px", "1fr"], "slots": ["ai", "health", "launcher"] }
]
```

A user layout with a built-in id overrides it; remove the entry to get the default back. `ui.layouts` sets the number-key order and the rotation cycle.

Other sections: `projects` (switcher and launcher target), `actions` (the only things the launcher can run: `terminal`, `open-app`, `open-url`, with optional `mac`/`windows` overrides), `display` (`fps`, `rotation`, `jpegQuality`, `brightness`), `led` (`enabled`, `maxBrightness`, `workingGlow`).

## Architecture

```
internal/collect/{system,claude,tools,demo}   telemetry (OS specifics in *_darwin.go / *_windows.go)
internal/server                                loopback API, SSE, embedded web UI (web/)
internal/display/bridge                        headless Chromium screencast of ?view=panel -> JPEG
internal/display/{us88,ledring}                device protocols over internal/usb (libusb on macOS, WinUSB on Windows)
internal/display/lights                        LED attention policy
internal/tray                                  menu bar / tray + global hotkeys
internal/hooks                                 Claude Code hook forwarder and installer
```

The UI is a web page. The bridge only captures it, so another screen needs only a new transport.

## What is real

| Data | Source |
| --- | --- |
| Sessions, busy/idle | `~/.claude/sessions/*.json`, written by Claude Code (undocumented, read only) |
| Approval waits, questions, failures, tool activity | Claude Code hooks; only event name, tool and file basename are forwarded |
| Task title, tokens, context | transcript `ai-title` and `usage` fields; message content is not decoded |
| Cost | not shown; no reliable local source yet |
| Codex, Cursor, Claude Desktop | process presence only: "running; task status unavailable" |
| Ollama | local API, loaded models |
| CPU, memory, disk, network, processes | macOS tools / gopsutil on Windows |
| GPU, thermal | macOS only for now; die temperatures need root and are not collected |

## Hardware notes

The screen enumerates as USB `1cbe:a088` and the LED ring as `0416:8050`. The frame protocol follows the open source [lian-li-linux](https://github.com/sgtaziz/lian-li-linux) driver. No firmware is modified, nothing is saved to the device, and the vendor "desktop mode" switch is not used. `pitwall probe` pushes an orientation frame; `probe led` blinks the ring.

If the panel stops taking frames, the bridge reconnects with backoff. If it stays stuck, unplug and replug the screen. Never USB-reset it: a reset takes it off the bus until it is power cycled.

**Windows (unverified):** this assumes Windows binds the WinUSB driver to the screen and ring, which the vendor driver's naming suggests. Quit L-Connect first, because it holds the devices. Then run `probe` to check.

## Security

Binds to 127.0.0.1 only and checks the Host header against DNS rebinding. Every POST needs the per-install token (0600, inlined into the served page) and a same-origin Origin. Request bodies are capped at 16 KB and actions are rate limited. State, logs and the event log live in `~/.local/state/pitwall` (macOS) or `%LOCALAPPDATA%\pitwall` (Windows). `PITWALL_PPROF=127.0.0.1:6060` enables profiling for development.

## Resource use

On an M3 Pro, measured by process CPU time: about 15 to 17% of one core while an agent is working (6 fps), and roughly half that when calm (2 fps). Most of it is the headless renderer; the Go process is about 3 to 5%.

## Next

1. Apply the brand kit.
2. Cost from the Claude Code status line JSON.
3. Codex session telemetry from `~/.codex/sessions`.
4. Windows GPU and thermal metrics; verify on real Windows hardware.
5. Start at login (LaunchAgent / Startup shortcut), opt in.
