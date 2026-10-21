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

// ResolveCommit returns the full SHA of a commit-ish in the repository.
func ResolveCommit(repoPath, revision string) (string, error) {
	if strings.HasPrefix(revision, "-") {
		return "", fmt.Errorf("invalid revision %q", revision)
	}
	out, err := exec.Command("git", "-C", repoPath, "rev-parse", "--verify", "--quiet", "--end-of-options", revision+"^{commit}").Output()
	if err != nil {
		return "", fmt.Errorf("commit %q not found", revision)
	}
	return strings.TrimSpace(string(out)), nil
}

// CreateDetachedWorktree checks out commit in a new detached worktree. It
// never creates or moves branches, so other worktrees are unaffected.
func CreateDetachedWorktree(repoPath, worktreePath, commit string) error {
	out, err := exec.Command("git", "-C", repoPath, "worktree", "add", "--detach", "--end-of-options", worktreePath, commit).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add --detach failed: %w\n%s", err, out)
	}
	return nil
}
