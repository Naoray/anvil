package cli

import (
	"os/exec"
	"testing"
)

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// detachHEAD detaches HEAD in a repo so that its current branch can be checked
// out in a worktree. This is needed because git refuses to check out a branch
// in a worktree if it's already checked out elsewhere.
func detachHEAD(t *testing.T, repoDir string) {
	t.Helper()
	cmd := exec.Command("git", "-C", repoDir, "checkout", "--detach")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("detaching HEAD: %v\n%s", err, string(output))
	}
}
