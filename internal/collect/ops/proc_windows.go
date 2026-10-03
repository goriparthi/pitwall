package ops

import "os/exec"

// killGroup keeps the default behavior on Windows: the command itself is killed on timeout.
func killGroup(cmd *exec.Cmd) {}
