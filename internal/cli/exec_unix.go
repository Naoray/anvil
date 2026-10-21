//go:build !windows

package cli

import (
	"fmt"
	"os"
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

// runChildIn replaces anvil with the child after changing to dir. Unlike
// runChild (anvil exec), argv[0] is resolved against the child's own PATH
// and directory.
func runChildIn(dir string, argv []string, env []string) error {
	path, err := lookPathIn(argv[0], dir, env)
	if err != nil {
		return childLookupError(argv[0], err)
	}
	if err := os.Chdir(dir); err != nil {
		return &ChildExitError{Code: 1, Message: fmt.Sprintf("changing to %s: %v", dir, err)}
	}
	if err := syscall.Exec(path, argv, env); err != nil {
		return &ChildExitError{Code: 126, Message: fmt.Sprintf("executing %s: %v", path, err)}
	}
	return nil
}
