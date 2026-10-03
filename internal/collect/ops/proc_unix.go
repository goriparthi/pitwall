//go:build !windows

package ops

import (
	"os/exec"
	"syscall"
)

// killGroup runs the command in its own process group so a timeout also stops anything it spawned (ssh, psql).
func killGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}
