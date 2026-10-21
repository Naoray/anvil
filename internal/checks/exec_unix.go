//go:build !windows

package checks

import (
	"os/exec"
	"syscall"
	"time"
)

// configureGroup runs the command in its own process group so a timeout or
// interruption terminates everything it spawned, not just the shell.
func configureGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second
}

// killGroup removes stragglers after the leader exited. The group ID cannot
// have been recycled while any member of the group is still alive.
func killGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		return // ESRCH: the group is already empty
	}
}
