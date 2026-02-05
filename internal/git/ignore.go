package git

import (
	"errors"
	"os/exec"
)

// IsIgnored reports whether the given path is ignored by git.
func IsIgnored(worktreePath, relativePath string) (bool, error) {
	cmd := exec.Command("git", "-C", worktreePath, "check-ignore", "-q", "--", relativePath)
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}

	return true, nil
}
