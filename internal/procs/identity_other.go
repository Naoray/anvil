//go:build !linux && !darwin

package procs

// StartIdentity is unavailable: without a start identity Anvil cannot
// distinguish its process from a recycled PID, so it never signals.
func StartIdentity(int) (string, error) {
	return "", ErrUnsupported
}
