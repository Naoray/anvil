package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReviewRefusesExistingUnlabelledWorktrees(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	for _, kind := range []string{"legacy", "skip-prepare", "main"} {
		t.Run(kind, func(t *testing.T) {
			f := newAgentFixture(t, lifecycleFixtureYAML, nil)
			path := f.repo
			switch kind {
			case "legacy":
				path = f.worktreePath("legacy")
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
				gitRun(t, f.repo, "worktree", "add", "-b", "legacy", path, "HEAD")
			case "skip-prepare":
				_, stderr, code := f.runJSON(f.repo, "ensure", "legacy", "--skip-prepare")
				require.Equal(t, 0, code, stderr)
				path = f.worktreePath("legacy")
			}
			require.NoError(t, os.WriteFile(filepath.Join(path, "README.md"), []byte("implementation WIP\n"), 0o644))
			beforeState, beforeErr := os.ReadFile(filepath.Join(path, ".anvil.local"))
			beforeGit := gitRun(t, path, "status", "--porcelain")
			branch := gitRun(t, path, "symbolic-ref", "HEAD")
			envelope, stderr, code := f.runJSON(f.repo, "ensure", path, "--review", "HEAD", "--task-id", "must-not-record")
			require.Equal(t, 2, code, stderr)
			require.Equal(t, "invalid_arguments", field(t, envelope, "error", "code"))
			require.Contains(t, field(t, envelope, "error", "message"), "not a review worktree")
			require.Equal(t, branch, gitRun(t, path, "symbolic-ref", "HEAD"))
			require.Equal(t, beforeGit, gitRun(t, path, "status", "--porcelain"))
			afterState, afterErr := os.ReadFile(filepath.Join(path, ".anvil.local"))
			if os.IsNotExist(beforeErr) {
				require.True(t, os.IsNotExist(afterErr), "refusal must not create state")
			} else {
				require.NoError(t, beforeErr)
				require.NoError(t, afterErr)
				require.Equal(t, beforeState, afterState)
			}
		})
	}
}
