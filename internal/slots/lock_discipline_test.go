package slots

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/lockfile"
)

// TestQueueFilesAreOnlyTouchedUnderQueueLock: Windows refuses to replace or
// delete a file another handle has open, so every read, replace and removal
// of queue.json and holder records must be serialized by queue.lock.
func TestQueueFilesAreOnlyTouchedUnderQueueLock(t *testing.T) {
	m := testManager(t, 1)
	var mu sync.Mutex
	var unlocked []string
	fileAccessHook = func(op, path string) {
		held, err := lockfile.IsHeld(filepath.Join(m.Dir, queueLock))
		if err != nil || !held {
			mu.Lock()
			unlocked = append(unlocked, op+" "+filepath.Base(path))
			mu.Unlock()
		}
	}
	t.Cleanup(func() { fileAccessHook = nil })

	holder := mustAcquire(t, m, Request{Weight: 1, Check: "holder"})
	_, err := m.Snapshot()
	require.NoError(t, err)
	_, err = m.Acquire(context.Background(), Request{Weight: 1, Check: "timeout", Wait: 30 * time.Millisecond})
	var busy *BusyError
	require.ErrorAs(t, err, &busy)
	require.Len(t, busy.Holders, 1)
	require.NoError(t, holder.Release())
	_, err = m.Snapshot()
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Empty(t, unlocked, "queue files accessed without queue.lock")
}

// lockedState reads the queue the way participants do: under queue.lock.
func lockedState(m *Manager) (queueState, error) {
	var state queueState
	err := m.withQueue(context.Background(), nil, func(current *queueState) (bool, error) {
		state = *current
		return false, nil
	})
	return state, err
}

// removeQueueFile simulates a lost queue file without racing participants.
func removeQueueFile(t *testing.T, m *Manager) {
	t.Helper()
	lock, err := lockfile.Acquire(filepath.Join(m.Dir, queueLock))
	require.NoError(t, err)
	defer func() { require.NoError(t, lock.Release()) }()
	require.NoError(t, os.Remove(filepath.Join(m.Dir, queueFile)))
}
