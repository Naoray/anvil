package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func childLookPathError(command string, lookupErr error) error {
	if lookupFailureMeansCannotExecute(command, lookupErr) {
		return &ChildExitError{
			Code:    126,
			Message: fmt.Sprintf("cannot execute %s: %v", command, lookupErr),
		}
	}
	return &ChildExitError{Code: 127, Message: fmt.Sprintf("command not found: %s", command)}
}

func lookupFailureMeansCannotExecute(command string, lookupErr error) bool {
	if errors.Is(lookupErr, fs.ErrPermission) {
		return true
	}

	if filepath.IsAbs(command) || strings.ContainsAny(command, `/\`) {
		return pathExistsOrIsBlocked(command)
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if pathExistsOrIsBlocked(filepath.Join(dir, command)) {
			return true
		}
	}
	return false
}

func pathExistsOrIsBlocked(path string) bool {
	if _, err := os.Stat(path); err == nil || !errors.Is(err, fs.ErrNotExist) {
		return true
	}
	_, err := os.Lstat(path)
	return err == nil || !errors.Is(err, fs.ErrNotExist)
}
