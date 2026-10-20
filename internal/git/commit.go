package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// HeadCommit returns the full commit SHA checked out in a worktree.
func HeadCommit(worktreePath string) (string, error) {
	out, err := exec.Command("git", "-C", worktreePath, "rev-parse", "--verify", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("resolving HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
