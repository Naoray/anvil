package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureResolvesMovedWorktreeByID(t *testing.T) {
	f := newAgentFixture(t, ensureFixtureYAML, nil)
	envelope, stderr, code := f.runJSON(f.repo, "ensure", "feature/id", "--skip-prepare")
	require.Equal(t, 0, code, stderr)
	id := field(t, envelope, "data", "worktree", "id").(string)
	moved := f.worktreePath("feature-moved")
	gitRun(t, f.repo, "worktree", "move", f.worktreePath("feature-id"), moved)
	envelope, stderr, code = f.runJSON(f.repo, "ensure", id, "--skip-prepare")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, moved, field(t, envelope, "data", "worktree", "path"))
	require.Equal(t, id, field(t, envelope, "data", "worktree", "id"))
	require.Equal(t, false, field(t, envelope, "data", "created"))
}
