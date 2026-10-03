// Command capture records the demo story as PNG frames for the README GIF and website video.
// It runs `pitwall run --demo` on a private port and state dir, then screenshots the panel view on a schedule.
// Usage: go run ./tools/capture -bin bin/pitwall -out /tmp/frames  (see scripts/capture-demo.sh)
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"github.com/goriparthi/pitwall/internal/display/bridge"
)

const port = 7790

// Story beats in capture seconds; demo story time is about capture time + 2 s
// (approval alert at 6 s, infra-7 completes at 12 s, approval clears at 18 s).
var beats = []struct {
	at   float64
	page string
}{
	{10, "agents"},
	{13, "usage"},
	{15.5, "balanced"},
}

func main() {
	bin := flag.String("bin", "bin/pitwall", "pitwall binary")
	out := flag.String("out", "frames", "output directory for PNG frames")
	fps := flag.Int("fps", 10, "frames per second")
	secs := flag.Float64("secs", 20, "capture length in seconds")
	flag.Parse()

	tmp, err := os.MkdirTemp("", "pitwall-capture-")
	check(err)
	defer os.RemoveAll(tmp)
	cfg := filepath.Join(tmp, "config.json")
	check(os.WriteFile(cfg, []byte(fmt.Sprintf(`{"server":{"port":%d},"display":{"enabled":false},
	  "actions":[{"id":"terminal","label":"Terminal","hint":"Terminal here","key":"t","kind":"terminal","app":"Terminal"},
	  {"id":"editor","label":"Editor","hint":"VS Code here","key":"e","kind":"open-app","app":"Visual Studio Code"},
	  {"id":"claude","label":"Claude Code","hint":"New session here","key":"c","kind":"terminal","app":"Terminal","run":"claude"},
	  {"id":"dashboard","label":"Full dashboard","hint":"Main monitor","key":"d","kind":"open-url","url":"http://127.0.0.1:%d/?view=full"},
	  {"id":"processes","label":"Processes","hint":"All processes","key":"a","kind":"open-app","app":"Activity Monitor"}]}`, port, port)), 0o600))

	srv := exec.Command(*bin, "run", "--demo", "--no-display", "--no-tray")
	srv.Env = append(os.Environ(), "PITWALL_CONFIG="+cfg, "PITWALL_STATE_DIR="+filepath.Join(tmp, "state"))
	check(srv.Start())
	defer func() {
		_ = srv.Process.Signal(os.Interrupt)
		_ = srv.Wait()
	}()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitHealthy(base)
	token, err := os.ReadFile(filepath.Join(tmp, "state", "token"))
	check(err)
	setPage := func(p string) {
		req, _ := http.NewRequest("POST", base+"/v1/ui", strings.NewReader(`{"page":"`+p+`","focus":false,"privacy":false,"rotate":false}`))
		req.Header.Set("x-pitwall-token", strings.TrimSpace(string(token)))
		if res, err := http.DefaultClient.Do(req); err == nil {
			res.Body.Close()
		}
	}
	setPage("balanced")

	opts := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(bridge.FindBrowser()), chromedp.WindowSize(1920, 480), chromedp.Flag("hide-scrollbars", true))
	actx, cancelA := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancelA()
	ctx, cancel := chromedp.NewContext(actx)
	defer cancel()
	check(chromedp.Run(ctx, chromedp.EmulateViewport(1920, 480), chromedp.Navigate(fmt.Sprintf("%s/?view=panel&fps=%d", base, *fps))))
	time.Sleep(2 * time.Second) // story time ~2 s at the first frame

	check(os.MkdirAll(*out, 0o755))
	interval := time.Second / time.Duration(*fps)
	total := int(*secs * float64(*fps))
	start := time.Now()
	next := 0
	for i := 0; i < total; i++ {
		due := start.Add(time.Duration(i) * interval)
		time.Sleep(time.Until(due))
		elapsed := time.Since(start).Seconds()
		for next < len(beats) && elapsed >= beats[next].at {
			setPage(beats[next].page)
			next++
		}
		var buf []byte
		check(chromedp.Run(ctx, chromedp.ActionFunc(func(c context.Context) error {
			var err error
			buf, err = page.CaptureScreenshot().WithFormat(page.CaptureScreenshotFormatPng).Do(c)
			return err
		})))
		check(os.WriteFile(filepath.Join(*out, fmt.Sprintf("frame_%04d.png", i)), buf, 0o644))
	}
	log.Printf("captured %d frames in %.1fs (target %.1fs)", total, time.Since(start).Seconds(), *secs)
}

func waitHealthy(base string) {
	for i := 0; i < 50; i++ {
		if res, err := http.Get(base + "/health"); err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	log.Fatal("demo server did not start")
}

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
