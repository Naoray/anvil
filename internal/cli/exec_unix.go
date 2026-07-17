//go:build !windows

package cli

import (
	"fmt"
	"os/exec"
	"syscall"
)

// runChild replaces the anvil process with the child command via execve, so
// the child directly owns the terminal, signals, stdio, and exit code.
func runChild(argv []string, env []string) error {
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return childLookPathError(argv[0], err)
	}
	if err := syscall.Exec(path, argv, env); err != nil {
		return &ChildExitError{Code: 126, Message: fmt.Sprintf("executing %s: %v", path, err)}
	}
	// Unreachable: a successful Exec never returns.
	return nil
}
