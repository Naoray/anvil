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

// TestQueueResetDoesNotStallWaiters: after queue.json is deleted, a rejoining
// waiter and a new waiter can carry the same sequence number. Each must still
// recognize its own ticket, or both believe they are second and nobody runs.
func TestQueueResetDoesNotStallWaiters(t *testing.T) {
	for _, holderSeq := range []bool{false, true} {
		m := testManager(t, 1)
		var holder interface{ Release() error }
		if holderSeq {
			holder = mustAcquire(t, m, Request{Weight: 1, Check: "holder"})
		} else {
			lock, err := lockfile.Acquire(m.slotLock(0)) // a holder that never queued here
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, lock.Release()) })
			holder = lock
		}
		var mu sync.Mutex
		var order []string
		errs := make(chan error, 2)
		waiter := func(name string) {
			held, err := m.Acquire(context.Background(), Request{Weight: 1, Check: name, Wait: 5 * time.Second})
			if err == nil {
				mu.Lock()
				order = append(order, name)
				mu.Unlock()
				time.Sleep(20 * time.Millisecond)
				err = held.Release()
			}
			errs <- err
		}
		go waiter("before-reset")
		waitQueued(t, m, 1)
		removeQueueFile(t, m)
		require.Eventually(t, func() bool { // the first waiter rejoins the empty queue
			_, err := os.Stat(filepath.Join(m.Dir, queueFile))
			return err == nil
		}, 5*time.Second, 2*time.Millisecond)
		go waiter("after-reset")
		waitQueued(t, m, 2)
		require.NoError(t, holder.Release())
		require.NoError(t, <-errs)
		require.NoError(t, <-errs)
		require.Equal(t, []string{"before-reset", "after-reset"}, order, "a rejoining waiter keeps its place")
		state, err := lockedState(m)
		require.NoError(t, err)
		require.Empty(t, state.Tickets)
	}
}

// TestLoadWaitIsBoundedPerTicketFromEnqueue: load_wait bounds each check's
// wait from when it queued, not anew for every successive head.
func TestLoadWaitIsBoundedPerTicketFromEnqueue(t *testing.T) {
	m := testManager(t, 2)
	m.MaxLoad, m.LoadWait = 1, 400*time.Millisecond
	m.ReadLoad = func() (float64, error) { return 9, nil }
	start := time.Now()
	var mu sync.Mutex
	var admitted []time.Duration
	var held []*Held
	var wg sync.WaitGroup
	for i, name := range []string{"first", "second"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h, err := m.Acquire(context.Background(), Request{Weight: 1, Check: name})
			if err != nil {
				t.Errorf("%s: %v", name, err)
				return
			}
			mu.Lock()
			admitted, held = append(admitted, time.Since(start)), append(held, h)
			mu.Unlock()
		}()
		waitQueued(t, m, i+1)
	}
	wg.Wait()
	require.Len(t, admitted, 2)
	require.Less(t, max(admitted[0], admitted[1]), 700*time.Millisecond, "both bounds run concurrently from enqueue: %v", admitted)
	for _, h := range held {
		require.NoError(t, h.Release())
	}
}

func TestMalformedQueueIsTreatedAsEmpty(t *testing.T) {
	for _, content := range []string{"{", ""} {
		m := testManager(t, 1)
		require.NoError(t, os.MkdirAll(m.Dir, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(m.Dir, queueFile), []byte(content), 0o600))
		status, err := m.Snapshot()
		require.Error(t, err, "read-only views report the reset as a diagnostic")
		require.Empty(t, status.Queue)

		var warnings []string
		held, err := m.Acquire(context.Background(), Request{Weight: 1, Check: "c", Wait: time.Second,
			Warn: func(w string) { warnings = append(warnings, w) }})
		require.NoError(t, err, "a corrupt queue only resets ordering; capacity is enforced by slot locks")
		require.Len(t, warnings, 1)
		require.Contains(t, warnings[0], "malformed slot queue")
		state, err := lockedState(m)
		require.NoError(t, err, "the next write repairs the queue")
		require.Empty(t, state.Tickets)
		require.NoError(t, held.Release())
	}
}

func TestCancellationIsPromptWhileQueueLockIsHeld(t *testing.T) {
	m := testManager(t, 1)
	lock, err := lockfile.Acquire(filepath.Join(m.Dir, queueLock))
	require.NoError(t, err)
	defer func() { require.NoError(t, lock.Release()) }()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = m.Acquire(ctx, Request{Weight: 1, Check: "c"})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(start), 1500*time.Millisecond)
}

func TestSlotWaitDeadlineIncludesQueueLockContention(t *testing.T) {
	shortQueueLockWait(t, 250*time.Millisecond)
	m := testManager(t, 1)
	holder := mustAcquire(t, m, Request{Weight: 1, Check: "holder"})
	defer func() { require.NoError(t, holder.Release()) }()
	queue := holdQueueLock(t, m)

	start := time.Now()
	_, err := m.Acquire(context.Background(), Request{Weight: 1, Check: "waiter", Wait: 50 * time.Millisecond})
	var busy *BusyError
	require.ErrorAs(t, err, &busy)
	require.Less(t, time.Since(start), 150*time.Millisecond, "queue-lock contention is inside the slot wait bound")
	require.Equal(t, 1, busy.InUse, "timeout diagnostics still count held slot locks")
	require.True(t, busy.QueueBusy, "the unavailable queue snapshot is identified")

	require.NoError(t, queue.Release())
	require.NoError(t, holder.Release())
}

func TestTryAcquireDoesNotWaitForQueueLock(t *testing.T) {
	shortQueueLockWait(t, 250*time.Millisecond)
	m := testManager(t, 1)
	holdQueueLock(t, m)
	start := time.Now()
	held, err := m.TryAcquire(context.Background(), Request{Weight: 1, Check: "try"})
	require.NoError(t, err)
	require.Nil(t, held)
	require.Less(t, time.Since(start), 50*time.Millisecond, "TryAcquire makes one non-blocking queue-lock attempt")
}

func TestQueueLockTimeoutIsRetried(t *testing.T) {
	previous := queueLockWait
	queueLockWait = 30 * time.Millisecond
	defer func() { queueLockWait = previous }()
	m := testManager(t, 1)
	lock, err := lockfile.Acquire(filepath.Join(m.Dir, queueLock))
	require.NoError(t, err)
	go func() {
		time.Sleep(300 * time.Millisecond)
		if err := lock.Release(); err != nil {
			t.Errorf("release: %v", err)
		}
	}()
	held, err := m.Acquire(context.Background(), Request{Weight: 1, Check: "c", Wait: 5 * time.Second})
	require.NoError(t, err, "a busy queue lock is retried at the next poll")
	require.NoError(t, held.Release())
}

func TestTryAcquireNeverWaitsAndRespectsTheQueue(t *testing.T) {
	m := testManager(t, 1)
	held, err := m.TryAcquire(context.Background(), Request{Weight: 1, Check: "free"})
	require.NoError(t, err)
	require.NotNil(t, held)

	none, err := m.TryAcquire(context.Background(), Request{Weight: 1, Check: "busy"})
	require.NoError(t, err)
	require.Nil(t, none)
	require.Equal(t, 0, queueLen(t, m), "an unsuccessful try leaves no ticket")

	errs := make(chan error, 1)
	go func() {
		h, err := m.Acquire(context.Background(), Request{Weight: 1, Check: "queued"})
		if err == nil {
			err = h.Release()
		}
		errs <- err
	}()
	waitQueued(t, m, 1)
	require.NoError(t, held.Release())
	require.NoError(t, <-errs)

	// Head-of-line: a free slot is not taken past an earlier queued request.
	wide := testManager(t, 2)
	holder := mustAcquire(t, wide, Request{Weight: 1, Check: "holder"})
	go func() {
		h, err := wide.Acquire(context.Background(), Request{Weight: 2, Check: "big"})
		if err == nil {
			err = h.Release()
		}
		errs <- err
	}()
	waitQueued(t, wide, 1)
	try, err := wide.TryAcquire(context.Background(), Request{Weight: 1, Check: "late"})
	require.NoError(t, err)
	require.Nil(t, try, "one slot is free but the earlier request comes first")
	require.Equal(t, 1, queueLen(t, wide))
	require.NoError(t, holder.Release())
	require.NoError(t, <-errs)
}
