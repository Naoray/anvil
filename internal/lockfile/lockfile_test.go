package lockfile

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTryAcquireContendsWithinProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "x.lock")
	first, err := Acquire(path)
	require.NoError(t, err)

	_, err = TryAcquire(path)
	require.ErrorIs(t, err, ErrLocked)
	held, err := IsHeld(path)
	require.NoError(t, err)
	require.True(t, held)

	start := time.Now()
	_, err = AcquireTimeout(path, 120*time.Millisecond)
	require.ErrorIs(t, err, ErrLocked)
	require.GreaterOrEqual(t, time.Since(start), 100*time.Millisecond)

	require.NoError(t, first.Release())
	require.NoError(t, first.Release(), "double release is a no-op")
	held, err = IsHeld(path)
	require.NoError(t, err)
	require.False(t, held)
	second, err := TryAcquire(path)
	require.NoError(t, err)
	require.NoError(t, second.Release())
}

func TestIsHeldMissingFile(t *testing.T) {
	held, err := IsHeld(filepath.Join(t.TempDir(), "missing.lock"))
	require.NoError(t, err)
	require.False(t, held)
}

// TestLockReleasedWhenHolderDies proves recovery after an interrupted holder
// relies on the operating system, never on elapsed time.
func TestLockReleasedWhenHolderDies(t *testing.T) {
	if path := os.Getenv("ANVIL_LOCKFILE_CHILD"); path != "" {
		_, err := Acquire(path)
		if err != nil {
			os.Exit(2)
		}
		if err := os.WriteFile(path+".ready", []byte("1"), 0o600); err != nil {
			os.Exit(3)
		}
		time.Sleep(time.Hour)
		return
	}
	path := filepath.Join(t.TempDir(), "holder.lock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockReleasedWhenHolderDies$")
	cmd.Env = append(os.Environ(), "ANVIL_LOCKFILE_CHILD="+path)
	require.NoError(t, cmd.Start())
	require.Eventually(t, func() bool {
		_, err := os.Stat(path + ".ready")
		return err == nil
	}, 10*time.Second, 10*time.Millisecond)

	_, err := TryAcquire(path)
	require.True(t, errors.Is(err, ErrLocked), "child must hold the lock: %v", err)

	require.NoError(t, cmd.Process.Kill())
	require.Error(t, cmd.Wait())

	lock, err := AcquireTimeout(path, 5*time.Second)
	require.NoError(t, err)
	require.NoError(t, lock.Release())
}
