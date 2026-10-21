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

// Start launches Command detached in its own session (so its PID is also its
// process group ID), with stdin from /dev/null and output appended to
// LogPath. The leader is this binary acting as supervisor: it runs
// `sh -c Command` and lives until the whole group has exited, so a command
// that backgrounds its work and returns still has a verifiable leader. The
// process keeps running after Anvil exits.
func Start(spec Spec) (Started, error) {
	logFile, err := os.OpenFile(spec.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return Started{}, fmt.Errorf("opening log: %w", err)
	}
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return Started{}, errors.Join(err, logFile.Close())
	}
	self, err := os.Executable()
	if err != nil {
		return Started{}, errors.Join(fmt.Errorf("locating supervisor: %w", err), logFile.Close(), devNull.Close())
	}
	cmd := exec.Command(self, SuperviseArg, spec.Command)
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
		// The supervisor already exited, so the command finished and left
		// no process in its group. The empty identity never verifies, so
		// nothing is ever signaled for this record.
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
// timeout. It signals only while the leader verifiably has its recorded
// identity. The supervisor leads every group Anvil starts until the group is
// empty, so a surviving group whose leader is gone cannot be proven ours: its
// ID may have been reused by an unrelated session, and it is never signaled.
func Stop(pid int, identity string, timeout time.Duration) (StopResult, error) {
	switch Check(pid, identity) {
	case StateReused:
		return StopNotOwned, nil
	case StateUnknown:
		return StopUnverifiable, nil
	case StateExited:
		if groupExists(pid) {
			return StopUnverifiable, nil
		}
		return StopAlreadyExited, nil
	case StateRunning:
	}
	if err := unix.Kill(-pid, unix.SIGTERM); err != nil && !errors.Is(err, unix.ESRCH) {
		return "", fmt.Errorf("signaling process group %d: %w", pid, err)
	}
	if waitGone(pid, identity, timeout) {
		return StopTerminated, nil
	}
	if Check(pid, identity) != StateRunning {
		// The leader exited after SIGTERM; without it the group is no
		// longer provably ours, so it is not escalated.
		return StopTerminated, nil
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
