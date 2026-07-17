//go:build windows

package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// runChild runs the child command with wired stdio and propagates its exit
// code as a typed error. Windows has no execve; anvil stays the parent
// process, so signal forwarding and job-object management are out of scope
// for v1.8 (documented limitation).
func runChild(argv []string, env []string) error {
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return childLookPathError(argv[0], err)
	}
	cmd := exec.Command(path, argv[1:]...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return &ChildExitError{Code: exitErr.ExitCode()}
		}
		return &ChildExitError{Code: 126, Message: fmt.Sprintf("executing %s: %v", path, err)}
	}
	return nil
}
