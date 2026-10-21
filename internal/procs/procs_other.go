//go:build !linux && !darwin

package procs

import "time"

// Supported reports whether managed processes work on this platform.
const Supported = false

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

// StopResult describes what Stop did.
type StopResult string

const (
	StopTerminated    StopResult = "terminated"
	StopKilled        StopResult = "killed"
	StopAlreadyExited StopResult = "already_exited"
	StopNotOwned      StopResult = "not_signaled_pid_reused"
	StopUnverifiable  StopResult = "not_signaled_unverifiable"
)

// Supervise never runs where managed processes are unsupported.
func Supervise([]string) (int, bool) { return 0, false }

// Start is unsupported without a verifiable process identity.
func Start(Spec) (Started, error) { return Started{}, ErrUnsupported }

// Stop never signals on platforms without verifiable identity.
func Stop(int, string, time.Duration) (StopResult, error) { return StopUnverifiable, nil }
