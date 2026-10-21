package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
)

func TestUnlinkCleanSkipsDeletedWorktree(t *testing.T) {
	f := newRemoveFixture(t)
	a, b := f.worktreePath("feature-a"), f.worktreePath("feature-b")
	require.NoError(t, os.RemoveAll(b))

	stdout, stderr, code := f.run(f.repo, "unlink", "demo", "--clean", "--dry-run")
	require.Equal(t, 0, code, stdout+stderr)
	require.Contains(t, stdout+stderr, "⚠ registered worktree "+b+" no longer exists; skipping it")
	require.Contains(t, stdout+stderr, "git worktree prune")
	require.Contains(t, stdout+stderr, "[DRY RUN] Would unlink project after worktree cleanup")
	require.DirExists(t, a)

	stdout, stderr, code = f.run(f.repo, "unlink", "demo", "--clean", "--force")
	require.Equal(t, 0, code, stdout+stderr)
	require.Contains(t, stdout+stderr, "git worktree prune")
	require.Contains(t, stdout+stderr, "Unlinked 'demo'")
	require.NoDirExists(t, a)
}

// Git never prunes a locked entry, so a locked worktree that is absent is
// expected back (for example on unmounted media) and must not be skipped.
func TestUnlinkCleanFailsClosedForMissingLockedWorktree(t *testing.T) {
	f := newRemoveFixture(t)
	a, b := f.worktreePath("feature-a"), f.worktreePath("feature-b")
	gitRun(t, f.repo, "worktree", "lock", "--reason", "usb", b)
	require.NoError(t, os.RemoveAll(b))

	for _, args := range [][]string{
		{"unlink", "demo", "--clean", "--dry-run"},
		{"unlink", "demo", "--clean", "--force"},
	} {
		stdout, stderr, code := f.run(f.repo, args...)
		require.NotEqual(t, 0, code, "%v", args)
		require.Contains(t, stdout+stderr, "registered worktree "+b+" is locked (usb) but absent; reconnect it, or unlock and prune it", "%v", args)
		require.DirExists(t, a, "%v", args)
	}
	stdout, stderr, code := f.run(f.repo, "context", "feature-a", "--json")
	require.Equal(t, 0, code, stdout+stderr, "project stays linked")
}

func TestUnlinkCleanFailsClosedForUnreadableOrMalformedTargetState(t *testing.T) {
	for name, corrupt := range map[string]func(t *testing.T, path string){
		"unreadable": func(t *testing.T, path string) {
			require.NoError(t, os.RemoveAll(path))
			require.NoError(t, os.Mkdir(path, 0o755))
		},
		"malformed": func(t *testing.T, path string) {
			require.NoError(t, os.WriteFile(path, []byte("bad: ["), 0o600))
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newRemoveFixture(t)
			a, b := f.worktreePath("feature-a"), f.worktreePath("feature-b")
			corrupt(t, filepath.Join(b, config.LocalStateFile))

			stdout, stderr, code := f.run(f.repo, "unlink", "demo", "--clean", "--force")
			require.NotEqual(t, 0, code)
			require.Contains(t, stdout+stderr, "reading cleanup state for")
			require.DirExists(t, a)
			require.DirExists(t, b)
		})
	}
}

// newPruneFixture links a project whose origin is a reachable bare clone and
// adds merged worktrees for each branch.
func newPruneFixture(t *testing.T, branches ...string) *agentFixture {
	t.Helper()
	f := newAgentFixture(t, "", nil)
	origin := filepath.Join(t.TempDir(), "origin.git")
	gitRun(t, f.repo, "clone", "--bare", f.repo, origin)
	gitRun(t, f.repo, "remote", "add", "origin", origin)
	gitRun(t, f.repo, "fetch", "origin")
	for _, branch := range branches {
		wt := f.worktreePath(filepath.Base(branch))
		require.NoError(t, os.MkdirAll(filepath.Dir(wt), 0o755))
		gitRun(t, f.repo, "worktree", "add", "-b", branch, wt)
	}
	return f
}

func TestPruneSkipsDeletedMergedWorktree(t *testing.T) {
	f := newPruneFixture(t, "feature/kept", "feature/gone")
	kept, gone := f.worktreePath("kept"), f.worktreePath("gone")
	require.NoError(t, os.RemoveAll(gone))

	stdout, stderr, code := f.run(f.repo, "prune", "--dry-run")
	require.Equal(t, 0, code, stdout+stderr)
	require.Contains(t, stdout+stderr, "⚠ registered worktree "+gone+" no longer exists; skipping it")
	require.Contains(t, stdout+stderr, "git worktree prune")
	require.DirExists(t, kept)

	stdout, stderr, code = f.run(f.repo, "prune", "--force")
	require.Equal(t, 0, code, stdout+stderr)
	require.Contains(t, stdout+stderr, "git worktree prune")
	require.NoDirExists(t, kept)
}
