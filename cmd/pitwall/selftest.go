package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/goriparthi/pitwall/internal/hooks"
)

// selftestCmd sends a synthetic "needs approval" session through the real hook forwarder, holds, then ends it:
// the panel should show the amber banner and the LED ring should breathe amber.
func selftestCmd(args []string) int {
	secs := 15
	if len(args) > 0 {
		if n, err := strconv.Atoi(args[0]); err == nil && n > 0 {
			secs = n
		}
	}
	if _, ok := health(); !ok {
		fmt.Fprintln(os.Stderr, "pitwall is not running; start it first")
		return 1
	}
	cwd, _ := os.Getwd()
	send := func(event, extra string) {
		hooks.Forward(strings.NewReader(fmt.Sprintf(`{"hook_event_name":%q,"session_id":"mc-selftest","cwd":%q%s}`, event, cwd, extra)))
	}
	send("SessionStart", `,"source":"startup"`)
	send("UserPromptSubmit", "")
	send("PermissionRequest", `,"tool_name":"Bash"`)
	fmt.Printf("self-test: fake session waiting for approval for %d s (amber banner, LED breathing)\n", secs)
	time.Sleep(time.Duration(secs) * time.Second)
	send("SessionEnd", `,"reason":"other"`)
	fmt.Println("self-test ended")
	return 0
}
