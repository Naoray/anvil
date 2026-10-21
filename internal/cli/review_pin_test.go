package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReviewReuseValidatesActualDetachedHead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	f := newAgentFixture(t, lifecycleFixtureYAML, nil)
	pin := gitRun(t, f.repo, "rev-parse", "HEAD")
	require.NoError(t, os.WriteFile(filepath.Join(f.repo, "README.md"), []byte("second candidate\n"), 0o644))
	gitRun(t, f.repo, "commit", "-am", "second candidate")
	other := gitRun(t, f.repo, "rev-parse", "HEAD")
	for _, kind := range []string{"moved-head", "attached-head"} {
		t.Run(kind, func(t *testing.T) {
			first, stderr, code := f.runJSON(f.repo, "ensure", kind, "--review", pin)
			require.Equal(t, 0, code, stderr)
			path := field(t, first, "data", "worktree", "path").(string)
			if kind == "moved-head" {
				gitRun(t, path, "checkout", "--detach", other)
			} else {
				gitRun(t, path, "checkout", "-b", "reviewer-branch", pin)
			}
			require.NoError(t, os.WriteFile(filepath.Join(path, "README.md"), []byte("reviewer edits\n"), 0o644))
			statePath := filepath.Join(path, ".anvil.local")
			before, err := os.ReadFile(statePath)
			require.NoError(t, err)
			beforeGit := gitRun(t, path, "status", "--porcelain", "--branch")
			for _, flags := range [][]string{{"--review", pin}, {}, {"--skip-prepare"}} {
				args := append([]string{"ensure", path}, flags...)
				envelope, stderr, code := f.runJSON(f.repo, args...)
				require.Equal(t, 2, code, stderr)
				require.Equal(t, "invalid_arguments", field(t, envelope, "error", "code"))
				require.Contains(t, field(t, envelope, "error", "message"), "detached")
				after, err := os.ReadFile(statePath)
				require.NoError(t, err)
				require.Equal(t, before, after, "refusal must preserve metadata and preparation state")
				require.Equal(t, beforeGit, gitRun(t, path, "status", "--porcelain", "--branch"))
			}
		})
	}
}
