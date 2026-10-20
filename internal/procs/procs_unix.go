//go:build linux || darwin

package procs

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Supported reports whether managed processes work on this platform.
const Supported = true

// Spec describes a process to start.
type Spec struct {
	Command string
	Dir     string
	Env     []string
	LogPath string
}

// Started is a launched process and its verified identity.
type Started struct {
	PID      int
	Identity string
}

// Start launches `sh -c Command` detached in its own session (so its PID is
// also its process group ID), with stdin from /dev/null and output appended
// to LogPath. The process keeps running after Anvil exits.
func Start(spec Spec) (Started, error) {
	logFile, err := os.OpenFile(spec.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return Started{}, fmt.Errorf("opening log: %w", err)
	}
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return Started{}, errors.Join(err, logFile.Close())
	}
	cmd := exec.Command("sh", "-c", spec.Command)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	cmd.Stdin = devNull
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	startErr := cmd.Start()
	closeErr := errors.Join(logFile.Close(), devNull.Close())
	if startErr != nil {
		return Started{}, errors.Join(fmt.Errorf("starting process: %w", startErr), closeErr)
	}
	pid := cmd.Process.Pid
	identity, err := StartIdentity(pid)
	if errors.Is(err, ErrNoProcess) {
		// The leader already exited (for example a command that backgrounds
		// work and returns). Its process group may still hold our
		// descendants: record it without an identity. Stop may then signal
		// only a surviving group of the dead leader; once the PID is reused
		// by anyone, the empty identity reads as unknown and is never
		// signaled.
		reap(cmd)
		return Started{PID: pid}, closeErr
	}
	if err != nil {
		// Without an identity the process can never be stopped safely, so
		// do not leave it running unrecorded.
		killErr := unix.Kill(-pid, unix.SIGKILL)
		return Started{}, errors.Join(fmt.Errorf("verifying started process: %w", err), killErr, closeErr, cmd.Wait())
	}
	reap(cmd)
	return Started{PID: pid, Identity: identity}, closeErr
}

// reap collects the child's exit status if it dies while this process is
// still alive, so it never lingers as a zombie of a long-lived caller.
func reap(cmd *exec.Cmd) {
	go func() {
		// The exit status is irrelevant: liveness is always re-verified by
		// identity, never inferred from this wait.
		if err := cmd.Wait(); err != nil {
			return
		}
	}()
}

// groupExists reports whether any process still has pgid as process group.
// A PID is never recycled while it is in use as a process group ID, so a
// surviving group of a dead leader still contains only its descendants.
func groupExists(pgid int) bool {
	err := unix.Kill(-pgid, 0)
	return err == nil || errors.Is(err, unix.EPERM)
}

// StopResult describes what Stop did.
type StopResult string

const (
	StopTerminated    StopResult = "terminated"
	StopKilled        StopResult = "killed"
	StopAlreadyExited StopResult = "already_exited"
	StopNotOwned      StopResult = "not_signaled_pid_reused"
	StopUnverifiable  StopResult = "not_signaled_unverifiable"
)

// Stop terminates a recorded process group: SIGTERM, then SIGKILL after
// timeout. It only signals when the leader still has its recorded identity,
// or when the leader is gone but its own process group survives.
func Stop(pid int, identity string, timeout time.Duration) (StopResult, error) {
	switch Check(pid, identity) {
	case StateReused:
		return StopNotOwned, nil
	case StateUnknown:
		return StopUnverifiable, nil
	case StateExited:
		if !groupExists(pid) {
			return StopAlreadyExited, nil
		}
	case StateRunning:
	}
	if err := unix.Kill(-pid, unix.SIGTERM); err != nil && !errors.Is(err, unix.ESRCH) {
		return "", fmt.Errorf("signaling process group %d: %w", pid, err)
	}
	if waitGone(pid, identity, timeout) {
		return StopTerminated, nil
	}
	switch Check(pid, identity) {
	case StateReused, StateUnknown:
		return StopTerminated, nil
	case StateRunning, StateExited:
	}
	if err := unix.Kill(-pid, unix.SIGKILL); err != nil && !errors.Is(err, unix.ESRCH) {
		return "", fmt.Errorf("killing process group %d: %w", pid, err)
	}
	if !waitGone(pid, identity, 5*time.Second) {
		return "", fmt.Errorf("process group %d survived SIGKILL", pid)
	}
	return StopKilled, nil
}

// waitGone polls until the leader has exited (or its PID was recycled) and
// its process group is empty.
func waitGone(pid int, identity string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		state := Check(pid, identity)
		if state != StateRunning && !groupExists(pid) {
			return true
		}
		if state == StateReused {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}
