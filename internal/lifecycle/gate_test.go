package lifecycle

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/lockfile"
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
