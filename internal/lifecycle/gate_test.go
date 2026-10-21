package lifecycle

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/lockfile"
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
