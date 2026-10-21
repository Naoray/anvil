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

// TestStaleTempFilesAreSwept: temp files of writers killed mid-write (kill -9)
// are removed once old; fresh ones may belong to a live writer.
func TestStaleTempFilesAreSwept(t *testing.T) {
	m := testManager(t, 1)
	require.NoError(t, os.MkdirAll(m.Dir, 0o700))
	stale, fresh := filepath.Join(m.Dir, ".slots-stale"), filepath.Join(m.Dir, ".slots-fresh")
	require.NoError(t, os.WriteFile(stale, []byte("{"), 0o600))
	require.NoError(t, os.WriteFile(fresh, []byte("{"), 0o600))
	old := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(stale, old, old))
	mustAcquire(t, m, Request{Weight: 1, Check: "c"})
	require.NoFileExists(t, stale)
	require.FileExists(t, fresh)
}

func shortQueueLockWait(t *testing.T, wait time.Duration) {
	t.Helper()
	previous := queueLockWait
	queueLockWait = wait
	t.Cleanup(func() { queueLockWait = previous })
}

func holdQueueLock(t *testing.T, m *Manager) *lockfile.Lock {
	t.Helper()
	lock, err := lockfile.Acquire(filepath.Join(m.Dir, queueLock))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lock.Release()) })
	return lock
}

// TestBusySnapshotStillCountsHeldSlots: a busy queue lock must not make the
// machine look idle.
func TestBusySnapshotStillCountsHeldSlots(t *testing.T) {
	shortQueueLockWait(t, 30*time.Millisecond)
	m := testManager(t, 2)
	mustAcquire(t, m, Request{Weight: 1, Check: "holder"})
	holdQueueLock(t, m)
	status, err := m.Snapshot()
	require.ErrorIs(t, err, errQueueBusy)
	require.True(t, status.QueueBusy)
	require.Equal(t, 1, status.InUse, "held slots are counted by probing their locks")
	require.Nil(t, status.Holders, "holders are unavailable, not empty")
	require.Nil(t, status.Queue, "the queue is unavailable, not empty")
}

// TestReleaseFreesSlotsBeforeWaitingForQueueLock: capacity is returned at
// once; holder records are tidied afterwards under queue.lock.
func TestReleaseFreesSlotsBeforeWaitingForQueueLock(t *testing.T) {
	shortQueueLockWait(t, 500*time.Millisecond)
	m := testManager(t, 1)
	held := mustAcquire(t, m, Request{Weight: 1, Check: "old"})
	queue := holdQueueLock(t, m)
	done := make(chan error, 1)
	go func() { done <- held.Release() }()

	// A new holder takes the freed slot and records itself while the old
	// holder still waits for queue.lock.
	var next *lockfile.Lock
	require.Eventually(t, func() bool {
		lock, err := lockfile.TryAcquire(m.slotLock(0))
		if err != nil {
			return false
		}
		next = lock
		return true
	}, 300*time.Millisecond, 5*time.Millisecond, "the slot is free before queue.lock is obtained")
	t.Cleanup(func() { require.NoError(t, next.Release()) })
	require.NoError(t, writeJSON(m.slotRecord(0), Holder{Slot: 0, PID: 1, Check: "new"}))
	require.NoError(t, queue.Release())
	require.NoError(t, <-done)

	content, err := os.ReadFile(m.slotRecord(0))
	require.NoError(t, err, "the new holder's record survives the old holder's cleanup")
	require.Contains(t, string(content), `"new"`)
}

// TestReleaseSkipsRecordRemovalWithoutQueueLock: when queue.lock cannot be
// taken for any reason the records stay (they are only shown for held slots).
func TestReleaseSkipsRecordRemovalWithoutQueueLock(t *testing.T) {
	m := testManager(t, 1)
	held := mustAcquire(t, m, Request{Weight: 1, Check: "c"})
	lockPath := filepath.Join(m.Dir, queueLock)
	require.NoError(t, os.Remove(lockPath))
	require.NoError(t, os.Mkdir(lockPath, 0o700)) // opening it fails, not ErrLocked
	t.Cleanup(func() { require.NoError(t, os.Remove(lockPath)) })
	require.Error(t, held.Release(), "the unexpected lock error is reported")
	require.FileExists(t, m.slotRecord(0))
	free, err := lockfile.TryAcquire(m.slotLock(0))
	require.NoError(t, err, "the slot itself is released")
	require.NoError(t, free.Release())
}
