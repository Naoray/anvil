//go:build darwin

package procs

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// sZOMB is the BSD zombie process state.
const sZOMB = 5

// StartIdentity returns "darwin:<start seconds>.<microseconds>".
func StartIdentity(pid int) (string, error) {
	if pid <= 0 {
		return "", ErrNoProcess
	}
	if err := unix.Kill(pid, 0); errors.Is(err, unix.ESRCH) {
		return "", ErrNoProcess
	}
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if errors.Is(err, unix.EIO) || errors.Is(err, unix.ESRCH) {
		return "", ErrNoProcess
	}
	if err != nil {
		return "", err
	}
	if info.Proc.P_pid != int32(pid) || info.Proc.P_stat == sZOMB {
		return "", ErrNoProcess
	}
	start := info.Proc.P_starttime
	return fmt.Sprintf("darwin:%d.%06d", start.Sec, start.Usec), nil
}
