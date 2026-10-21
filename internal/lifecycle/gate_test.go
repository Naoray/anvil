package lifecycle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/lockfile"
	"github.com/naoray/anvil/internal/wtstate"
)

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
