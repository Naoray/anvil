// Package procs starts and stops managed development processes. A process is
// identified by its PID together with its kernel start time, so a recycled
// PID is never mistaken for the process Anvil started. Processes are never
// signaled because of a port they listen on.
package procs

import "errors"

var (
	// ErrNoProcess reports that no live process has the PID.
	ErrNoProcess = errors.New("process does not exist")
	// ErrUnsupported reports that process identity cannot be verified here.
	ErrUnsupported = errors.New("managed processes are not supported on this platform")
)

// State is the verified state of a recorded process.
type State string

const (
	// StateRunning: the PID is alive with the recorded start identity.
	StateRunning State = "running"
	// StateExited: the leader is gone (its process group may linger).
	StateExited State = "exited"
	// StateReused: the PID now belongs to a different process.
	StateReused State = "reused"
	// StateUnknown: identity cannot be verified on this platform.
	StateUnknown State = "unknown"
)

// Check classifies a recorded process without signaling it.
func Check(pid int, identity string) State {
	current, err := StartIdentity(pid)
	switch {
	case errors.Is(err, ErrNoProcess):
		return StateExited
	case err != nil || identity == "":
		return StateUnknown
	case current != identity:
		return StateReused
	default:
		return StateRunning
	}
}
