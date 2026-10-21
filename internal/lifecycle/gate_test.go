package lifecycle

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/lockfile"
	"github.com/naoray/anvil/internal/wtstate"
)

func TestGateSharesUsersAndExcludesCleanup(t *testing.T) {
	root := t.TempDir()
	out, err := exec.Command("git", "init", root).CombinedOutput()
	require.NoError(t, err, string(out))
	first, err := AcquireShared(root)
	require.NoError(t, err)
	second, err := AcquireShared(root)
	require.NoError(t, err)
	_, err = TryExclusive(root)
	require.ErrorIs(t, err, lockfile.ErrLocked)
	require.NoError(t, first.Release())
	_, err = TryExclusive(root)
	require.ErrorIs(t, err, lockfile.ErrLocked)
	require.NoError(t, second.Release())
	cleanup, err := TryExclusive(root)
	require.NoError(t, err)
	_, err = AcquireShared(root)
	require.ErrorIs(t, err, lockfile.ErrLocked)
	path, err := Path(root)
	require.NoError(t, err)
	require.NoError(t, cleanup.Release())
	require.FileExists(t, path)
	if alias := filepath.Join(t.TempDir(), "alias"); os.Symlink(root, alias) == nil {
		aliased, err := Path(alias)
		require.NoError(t, err)
		require.Equal(t, path, aliased)
	}
}

func TestGateAllowsConcurrentUsersAndExcludesCleanup(t *testing.T) {
	path := t.TempDir()
	first, err := AcquireShared(path)
	require.NoError(t, err)
	second, err := AcquireShared(path)
	require.NoError(t, err)
	_, err = TryExclusive(path)
	require.ErrorIs(t, err, lockfile.ErrLocked)
	require.NoError(t, second.Release())
	_, err = TryExclusive(path)
	require.ErrorIs(t, err, lockfile.ErrLocked)
	require.NoError(t, first.Release())

	cleanup, err := TryExclusive(path)
	require.NoError(t, err)
	_, err = AcquireShared(path)
	require.ErrorIs(t, err, lockfile.ErrLocked)
	require.NoError(t, cleanup.Release())
	user, err := AcquireShared(path)
	require.NoError(t, err)
	require.NoError(t, user.Release())
}

func TestGateAndPrepareLocksSurviveWorktreeDeletion(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	path := filepath.Join(root, "worktree")
	require.NoError(t, os.MkdirAll(filepath.Join(path, ".git"), 0o700))
	gate, err := TryExclusive(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, gate.Release()) })
	preparePath := wtstate.PrepareLockPath(path)
	prepare, err := lockfile.TryAcquire(preparePath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, prepare.Release()) })

	// Cleanup must be able to remove the whole directory while both locks
	// remain open, including on Windows.
	require.NoError(t, os.RemoveAll(path))
	gatePath, err := Path(path)
	require.NoError(t, err)
	require.Equal(t, gate.Path(), gatePath)
	require.Equal(t, preparePath, wtstate.PrepareLockPath(path))
	_, err = AcquireShared(path)
	require.ErrorIs(t, err, lockfile.ErrLocked)
	_, err = lockfile.TryAcquire(preparePath)
	require.ErrorIs(t, err, lockfile.ErrLocked)
	require.NoDirExists(t, path, "contenders must not recreate the removed worktree")
}
