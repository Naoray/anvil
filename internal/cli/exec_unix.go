//go:build !windows

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// runChildIn replaces anvil with the child after changing to dir.
func runChildIn(dir string, argv []string, env []string) error {
	if err := os.Chdir(dir); err != nil {
		return &ChildExitError{Code: 1, Message: fmt.Sprintf("changing to %s: %v", dir, err)}
	}
	path, err := lookPathIn(dir, argv[0], env)
	if err != nil {
		return err
	}
	if err := syscall.Exec(path, argv, env); err != nil {
		return &ChildExitError{Code: 126, Message: fmt.Sprintf("executing %s: %v", path, err)}
	}
	return nil
}

// lookPathIn resolves against the child's PATH after runChildIn changes cwd.
func lookPathIn(_ string, name string, env []string) (string, error) {
	if strings.ContainsRune(name, '/') {
		path, err := exec.LookPath(name)
		if err != nil {
			return "", childLookPathError(name, err)
		}
		return path, nil
	}
	blocked := false
	for _, dir := range filepath.SplitList(childEnvValue(env, "PATH", false)) {
		if dir == "" {
			dir = "."
		}
		candidate, err := filepath.Abs(filepath.Join(dir, name))
		if err != nil {
			return "", err
		}
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
		blocked = blocked || pathExistsOrIsBlocked(candidate)
	}
	code := 127
	if blocked {
		code = 126
	}
	return "", &ChildExitError{Code: code, Message: fmt.Sprintf("cannot resolve command: %s", name)}
}
