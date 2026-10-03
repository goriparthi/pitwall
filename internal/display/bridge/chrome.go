package bridge

import (
	"os"
	"path/filepath"
	"runtime"
)

// FindBrowser locates a Chromium-based browser for headless rendering; PITWALL_CHROME overrides.
func FindBrowser() string {
	if p := os.Getenv("PITWALL_CHROME"); p != "" {
		return p
	}
	var candidates []string
	if runtime.GOOS == "windows" {
		for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LOCALAPPDATA")} {
			if base == "" {
				continue
			}
			candidates = append(candidates,
				filepath.Join(base, `Google\Chrome\Application\chrome.exe`),
				filepath.Join(base, `Microsoft\Edge\Application\msedge.exe`),
				filepath.Join(base, `BraveSoftware\Brave-Browser\Application\brave.exe`))
		}
	} else {
		home, _ := os.UserHomeDir()
		for _, base := range []string{"/Applications", filepath.Join(home, "Applications")} {
			candidates = append(candidates,
				filepath.Join(base, "Google Chrome.app/Contents/MacOS/Google Chrome"),
				filepath.Join(base, "Microsoft Edge.app/Contents/MacOS/Microsoft Edge"),
				filepath.Join(base, "Brave Browser.app/Contents/MacOS/Brave Browser"),
				filepath.Join(base, "Chromium.app/Contents/MacOS/Chromium"))
		}
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}
