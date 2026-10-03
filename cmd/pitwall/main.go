// pitwall: glanceable agent and system dashboard for the Lian Li 8.8" Universal Screen (macOS, Windows).
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/goriparthi/pitwall/internal/config"
	"github.com/goriparthi/pitwall/internal/hooks"
)

const usage = `usage: pitwall <command>

  run [--demo] [--no-display] [--no-tray]   run in the foreground
  start [same flags]                         run in the background
  stop | status | open                       control a background instance
  hooks install|uninstall|status             Claude Code hook entries (backs up settings.json)
  hook                                       hook forwarder (called by Claude Code)
  probe [led]                                push a test frame / LED test
  selftest [seconds]                         fake "needs approval" session end to end
  config                                     print the config file path`

func main() {
	if len(os.Args) < 2 {
		// launched from Finder as Pitwall.app: run instead of printing usage
		if strings.Contains(exe(), ".app/Contents/MacOS/") {
			os.Exit(runCmd(nil))
		}
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "hook":
		hooks.Forward(os.Stdin)
	case "run":
		os.Exit(runCmd(args))
	case "start":
		os.Exit(startCmd(args))
	case "stop":
		os.Exit(stopCmd())
	case "status":
		os.Exit(statusCmd())
	case "open":
		openBrowser(baseURL() + "/?view=full")
	case "hooks":
		os.Exit(hooksCmd(args))
	case "probe":
		os.Exit(probeCmd(args))
	case "selftest":
		os.Exit(selftestCmd(args))
	case "config":
		fmt.Println(config.Path())
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
}

type runFlags struct{ demo, noDisplay, noTray bool }

func parseRun(args []string) runFlags {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	var f runFlags
	fs.BoolVar(&f.demo, "demo", false, "synthetic data, actions disabled")
	fs.BoolVar(&f.noDisplay, "no-display", false, "do not drive the USB screen or LED ring")
	fs.BoolVar(&f.noTray, "no-tray", false, "no menu bar / tray item or global hotkeys")
	_ = fs.Parse(args)
	return f
}

func baseURL() string {
	port := 7788
	if s, err := config.Load(); err == nil {
		port = s.Get().Server.Port
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

func exe() string {
	p, err := os.Executable()
	if err != nil {
		return os.Args[0]
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func health() (string, bool) {
	c := http.Client{Timeout: 1500 * time.Millisecond}
	res, err := c.Get(baseURL() + "/health")
	if err != nil {
		return "", false
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return strings.TrimSpace(string(b)), res.StatusCode == 200
}

func startCmd(args []string) int {
	if h, ok := health(); ok {
		fmt.Println("already running:", h)
		return 0
	}
	logf, err := os.OpenFile(filepath.Join(config.StateDir(), "stdout.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	c := exec.Command(exe(), append([]string{"run"}, args...)...)
	c.Stdout, c.Stderr = logf, logf
	detach(c)
	if err := c.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "start failed:", err)
		return 1
	}
	for i := 0; i < 30; i++ {
		time.Sleep(200 * time.Millisecond)
		if h, ok := health(); ok {
			fmt.Printf("started (pid %d): %s\n  %s/?view=full\n", c.Process.Pid, h, baseURL())
			return 0
		}
	}
	fmt.Fprintln(os.Stderr, "did not become healthy; see", filepath.Join(config.StateDir(), "stdout.log"))
	return 1
}

func stopCmd() int {
	tok, err := os.ReadFile(config.TokenFile())
	if err != nil {
		fmt.Println("not running")
		return 0
	}
	req, _ := http.NewRequest("POST", baseURL()+"/v1/shutdown", strings.NewReader("{}"))
	req.Header.Set("x-pitwall-token", strings.TrimSpace(string(tok)))
	c := http.Client{Timeout: 2 * time.Second}
	res, err := c.Do(req)
	if err != nil {
		fmt.Println("not running")
		return 0
	}
	res.Body.Close()
	for i := 0; i < 50; i++ {
		time.Sleep(100 * time.Millisecond)
		if _, ok := health(); !ok {
			fmt.Println("stopped")
			return 0
		}
	}
	fmt.Fprintln(os.Stderr, "still running after 5 s")
	return 1
}

func statusCmd() int {
	if h, ok := health(); ok {
		fmt.Println("running:", h)
		return 0
	}
	fmt.Println("not running")
	return 1
}

func hooksCmd(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: pitwall hooks install|uninstall|status")
		return 2
	}
	switch args[0] {
	case "install":
		b, err := hooks.Install(exe())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Printf("installed hooks for %d events -> %s hook\nbackup: %s\n", len(hooks.Events), exe(), b)
	case "uninstall":
		b, err := hooks.Uninstall(exe())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("removed Pitwall hooks; backup:", b)
	case "status":
		if f := hooks.Status(exe()); len(f) > 0 {
			fmt.Println("installed for:", strings.Join(f, ", "))
		} else {
			fmt.Println("not installed")
		}
	default:
		return 2
	}
	return 0
}

func openBrowser(url string) {
	if runtime.GOOS == "windows" {
		_ = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url).Start()
		return
	}
	_ = exec.Command("open", url).Start()
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
