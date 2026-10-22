package slots

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/procs"
)

func testManager(t *testing.T, capacity int) *Manager {
	t.Helper()
	m := New(filepath.Join(t.TempDir(), "slots"), Settings{Capacity: capacity})
	m.Poll = 5 * time.Millisecond
	return m
}

func mustAcquire(t *testing.T, m *Manager, req Request) *Held {
	t.Helper()
	held, err := m.Acquire(context.Background(), req)
	require.NoError(t, err)
	// Release before TempDir removal even when the test fails early: Windows
	// cannot delete a lock file that is still open.
	t.Cleanup(func() { require.NoError(t, held.Release()) })
	return held
}

func queueLen(t *testing.T, m *Manager) int {
	t.Helper()
	status, err := m.Snapshot()
	require.NoError(t, err)
	return len(status.Queue)
}

// waitQueued blocks until n tickets are queued, so enqueue order is fixed.
func waitQueued(t *testing.T, m *Manager, n int) {
	t.Helper()
	require.Eventually(t, func() bool { return queueLen(t, m) == n }, 5*time.Second, 2*time.Millisecond)
}

func TestAcquireWithinCapacityTimesOutWithDiagnostics(t *testing.T) {
	m := testManager(t, 2)
	first := mustAcquire(t, m, Request{Weight: 1, Check: "a", Worktree: "/w/a"})
	second := mustAcquire(t, m, Request{Weight: 1, Check: "b", Worktree: "/w/b"})
	require.Len(t, first.Locks(), 1)

	start := time.Now()
	_, err := m.Acquire(context.Background(), Request{Weight: 1, Check: "c", Wait: 80 * time.Millisecond})
	var busy *BusyError
	require.ErrorAs(t, err, &busy)
	require.GreaterOrEqual(t, time.Since(start), 80*time.Millisecond)
	require.Equal(t, WaitingForSlot, busy.WaitingFor)
	require.Equal(t, 1, busy.Position)
	require.Equal(t, 1, busy.Queued)
	require.Len(t, busy.Holders, 2)
	require.ElementsMatch(t, []string{"a", "b"}, []string{busy.Holders[0].Check, busy.Holders[1].Check})
	require.Equal(t, 0, queueLen(t, m), "a timed-out waiter removes its ticket")

	status, err := m.Snapshot()
	require.NoError(t, err)
	require.Equal(t, 2, status.Capacity)
	require.Equal(t, 2, status.InUse)

	require.NoError(t, first.Release())
	third := mustAcquire(t, m, Request{Weight: 1, Check: "c"})
	require.NoError(t, errors.Join(second.Release(), third.Release()))
	status, err = m.Snapshot()
	require.NoError(t, err)
	require.Equal(t, 0, status.InUse)
	require.Empty(t, status.Holders)
}

// admissionOrder releases the single slot and records who gets it next.
func admissionOrder(t *testing.T, m *Manager, requests []Request) []string {
	t.Helper()
	blocker := mustAcquire(t, m, Request{Weight: 1, Check: "blocker"})
	var mu sync.Mutex
	var order []string
	var wg sync.WaitGroup
	errs := make(chan error, len(requests))
	for i, req := range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			held, err := m.Acquire(context.Background(), req)
			if err != nil {
				errs <- err
				return
			}
			mu.Lock()
			order = append(order, req.Check)
			mu.Unlock()
			time.Sleep(10 * time.Millisecond)
			errs <- held.Release()
		}()
		waitQueued(t, m, i+1)
	}
	require.NoError(t, blocker.Release())
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	return order
}

func TestFIFOOrder(t *testing.T) {
	m := testManager(t, 1)
	order := admissionOrder(t, m, []Request{{Weight: 1, Check: "first"}, {Weight: 1, Check: "second"}, {Weight: 1, Check: "third"}})
	require.Equal(t, []string{"first", "second", "third"}, order)
}

func TestPriorityOrderThenFIFO(t *testing.T) {
	m := testManager(t, 1)
	order := admissionOrder(t, m, []Request{
		{Weight: 1, Check: "low"}, {Weight: 1, Check: "high", Priority: 5},
		{Weight: 1, Check: "mid", Priority: 1}, {Weight: 1, Check: "high2", Priority: 5},
	})
	require.Equal(t, []string{"high", "high2", "mid", "low"}, order)
}

func TestHeadOfLineBlocksLaterSmallerRequests(t *testing.T) {
	m := testManager(t, 2)
	holder := mustAcquire(t, m, Request{Weight: 1, Check: "holder"})
	bigDone := make(chan *Held, 1)
	bigErr := make(chan error, 1)
	go func() {
		held, err := m.Acquire(context.Background(), Request{Weight: 2, Check: "big"})
		bigErr <- err
		bigDone <- held
	}()
	waitQueued(t, m, 1)

	// One slot is free, but the earlier weight-2 request is at the head.
	smallCtx, cancelSmall := context.WithCancel(context.Background())
	defer cancelSmall()
	type acquireResult struct {
		held *Held
		err  error
	}
	smallResult := make(chan acquireResult, 1)
	go func() {
		held, err := m.Acquire(smallCtx, Request{Weight: 1, Check: "small"})
		smallResult <- acquireResult{held: held, err: err}
	}()
	waitQueued(t, m, 2)

	status, err := m.Snapshot()
	require.NoError(t, err)
	require.Equal(t, "big", status.Queue[0].Check)
	require.Equal(t, "small", status.Queue[1].Check)

	cancelSmall()
	small := <-smallResult
	require.ErrorIs(t, small.err, context.Canceled)
	require.Nil(t, small.held)

	require.NoError(t, holder.Release())
	require.NoError(t, <-bigErr)
	big := <-bigDone
	require.Len(t, big.Locks(), 2)
	require.NoError(t, big.Release())
}

func TestCancellationRemovesTicket(t *testing.T) {
	m := testManager(t, 1)
	holder := mustAcquire(t, m, Request{Weight: 1, Check: "holder"})
	ctx, cancel := context.WithCancel(context.Background())
	errs := make(chan error, 1)
	go func() {
		_, err := m.Acquire(ctx, Request{Weight: 1, Check: "waiter"})
		errs <- err
	}()
	waitQueued(t, m, 1)
	cancel()
	require.ErrorIs(t, <-errs, context.Canceled)
	require.Equal(t, 0, queueLen(t, m))
	require.NoError(t, holder.Release())
}

func TestWeightExceedingCapacityFailsClearly(t *testing.T) {
	m := testManager(t, 2)
	_, err := m.Acquire(context.Background(), Request{Weight: 3, Check: "huge"})
	var capErr *CapacityError
	require.ErrorAs(t, err, &capErr)
	require.Equal(t, 3, capErr.Weight)
	require.Equal(t, 2, capErr.Capacity)
	require.Contains(t, err.Error(), "3 slots")
	require.Equal(t, 0, queueLen(t, m))
}

func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	require.NoError(t, cmd.Run())
	return cmd.Process.Pid
}

func TestStaleTicketsAreRemoved(t *testing.T) {
	if _, err := procs.StartIdentity(os.Getpid()); err != nil {
		t.Skip("process start identity unsupported on this platform")
	}
	m := testManager(t, 1)
	now := time.Now().UTC()
	require.NoError(t, m.writeState(queueState{NextSeq: 3, Tickets: []Ticket{
		{Seq: 1, PID: deadPID(t), StartIdentity: "x", Check: "killed", Priority: 9, Weight: 1, EnqueuedAt: now, Heartbeat: now},
		// PID reuse: a live PID whose kernel start identity no longer matches.
		{Seq: 2, PID: os.Getpid(), StartIdentity: "linux:other-boot:1", Check: "recycled", Priority: 9, Weight: 1, EnqueuedAt: now, Heartbeat: now},
	}}))
	status, err := m.Snapshot()
	require.NoError(t, err)
	require.Empty(t, status.Queue, "read-only views omit dead tickets")

	held, err := m.Acquire(context.Background(), Request{Weight: 1, Check: "live", Wait: 2 * time.Second})
	require.NoError(t, err, "stale tickets never block the queue")
	state, err := lockedState(m)
	require.NoError(t, err)
	require.Empty(t, state.Tickets, "participants delete stale tickets")
	require.NoError(t, held.Release())
}

func TestUnresponsiveTicketLosesHeadOfLineButStays(t *testing.T) {
	identity, err := procs.StartIdentity(os.Getpid())
	if err != nil {
		t.Skip("process start identity unsupported on this platform")
	}
	m := testManager(t, 1)
	m.StaleAfter = 50 * time.Millisecond
	old := time.Now().UTC().Add(-time.Minute)
	require.NoError(t, m.writeState(queueState{NextSeq: 2, Tickets: []Ticket{
		{Seq: 1, PID: os.Getpid(), StartIdentity: identity, Check: "frozen", Priority: 9, Weight: 1, EnqueuedAt: old, Heartbeat: old},
	}}))
	held, err := m.Acquire(context.Background(), Request{Weight: 1, Check: "live", Wait: 2 * time.Second})
	require.NoError(t, err)
	state, err := lockedState(m)
	require.NoError(t, err)
	require.Len(t, state.Tickets, 1, "a live but unresponsive ticket is kept")
	status, err := m.Snapshot()
	require.NoError(t, err)
	require.Len(t, status.Queue, 1)
	require.False(t, status.Queue[0].Responsive)
	require.NoError(t, held.Release())
}

func TestStaleHolderRecordIsIgnored(t *testing.T) {
	m := testManager(t, 1)
	require.NoError(t, os.MkdirAll(m.Dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(m.Dir, "slot-0.lock"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(m.Dir, "slot-0.holder.json"), []byte(`{"slot":0,"pid":1,"check":"ghost"}`), 0o600))
	status, err := m.Snapshot()
	require.NoError(t, err)
	require.Empty(t, status.Holders, "an unlocked slot has no holder whatever its record says")
	held := mustAcquire(t, m, Request{Weight: 1, Check: "live", Wait: time.Second})
	status, err = m.Snapshot()
	require.NoError(t, err)
	require.Len(t, status.Holders, 1)
	require.Equal(t, "live", status.Holders[0].Check)
	require.Equal(t, os.Getpid(), status.Holders[0].PID)
	require.NoError(t, held.Release())
}

// TestKilledHolderFreesCapacity: the OS releases slot locks of a killed
// holder; nothing depends on elapsed time.
func TestKilledHolderFreesCapacity(t *testing.T) {
	if dir := os.Getenv("ANVIL_SLOTS_CHILD"); dir != "" {
		m := New(dir, Settings{Capacity: 1})
		if _, err := m.Acquire(context.Background(), Request{Weight: 1, Check: "child"}); err != nil {
			os.Exit(2)
		}
		if err := os.WriteFile(filepath.Join(dir, "ready"), nil, 0o600); err != nil {
			os.Exit(3)
		}
		time.Sleep(time.Hour)
		return
	}
	if runtime.GOOS == "windows" {
		t.Skip("uses process kill semantics")
	}
	m := testManager(t, 1)
	cmd := exec.Command(os.Args[0], "-test.run=^TestKilledHolderFreesCapacity$")
	cmd.Env = append(os.Environ(), "ANVIL_SLOTS_CHILD="+m.Dir)
	require.NoError(t, cmd.Start())
	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(m.Dir, "ready"))
		return err == nil
	}, 10*time.Second, 10*time.Millisecond)
	status, err := m.Snapshot()
	require.NoError(t, err)
	require.Len(t, status.Holders, 1)
	require.Equal(t, cmd.Process.Pid, status.Holders[0].PID)

	_, err = m.Acquire(context.Background(), Request{Weight: 1, Check: "parent", Wait: 50 * time.Millisecond})
	var busy *BusyError
	require.ErrorAs(t, err, &busy)

	require.NoError(t, cmd.Process.Kill())
	require.Error(t, cmd.Wait())
	held, err := m.Acquire(context.Background(), Request{Weight: 1, Check: "parent", Wait: 5 * time.Second})
	require.NoError(t, err)
	require.NoError(t, held.Release())
}

func TestProgressReportsPosition(t *testing.T) {
	m := testManager(t, 1)
	holder := mustAcquire(t, m, Request{Weight: 1, Check: "holder"})
	errs := make(chan error, 2)
	go func() {
		held, err := m.Acquire(context.Background(), Request{Weight: 1, Check: "first"})
		if err == nil {
			time.Sleep(300 * time.Millisecond) // the second waiter observes position 1
			err = held.Release()
		}
		errs <- err
	}()
	waitQueued(t, m, 1)
	var mu sync.Mutex
	var seen []Progress
	go func() {
		held, err := m.Acquire(context.Background(), Request{Weight: 1, Check: "second", Progress: func(p Progress) {
			mu.Lock()
			seen = append(seen, p)
			mu.Unlock()
		}})
		if err == nil {
			err = held.Release()
		}
		errs <- err
	}()
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(seen) > 0
	}, 5*time.Second, 2*time.Millisecond)
	require.NoError(t, holder.Release())
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, Progress{WaitingFor: WaitingForSlot, Position: 2, Queued: 2}, seen[0])
	require.Equal(t, 1, seen[len(seen)-1].Position, "%+v", seen)
}

func loadManager(t *testing.T, readings func() (float64, error)) (*Manager, *[]string) {
	t.Helper()
	m := testManager(t, 1)
	m.MaxLoad = 4
	m.LoadWait = 150 * time.Millisecond
	m.ReadLoad = readings
	var warnings []string
	return m, &warnings
}

func TestLoadGatingWaitsThenProceedsWithWarning(t *testing.T) {
	m, warnings := loadManager(t, func() (float64, error) { return 9.5, nil })
	var mu sync.Mutex
	var progress []Progress
	start := time.Now()
	held, err := m.Acquire(context.Background(), Request{Weight: 1, Check: "bench",
		Warn: func(w string) { *warnings = append(*warnings, w) },
		Progress: func(p Progress) {
			mu.Lock()
			progress = append(progress, p)
			mu.Unlock()
		}})
	require.NoError(t, err, "slot capacity still caps concurrency once the load bound elapses")
	require.GreaterOrEqual(t, time.Since(start), 150*time.Millisecond)
	require.Len(t, *warnings, 1)
	require.Contains(t, (*warnings)[0], "9.50")
	mu.Lock()
	require.Equal(t, WaitingForLoad, progress[0].WaitingFor)
	mu.Unlock()
	require.NoError(t, held.Release())
}

func TestLoadGatingProceedsWhenLoadDrops(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	m, warnings := loadManager(t, func() (float64, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls < 3 {
			return 8, nil
		}
		return 1, nil
	})
	m.LoadWait = time.Minute
	held, err := m.Acquire(context.Background(), Request{Weight: 1, Check: "bench", Warn: func(w string) { *warnings = append(*warnings, w) }})
	require.NoError(t, err)
	require.Empty(t, *warnings)
	require.NoError(t, held.Release())
}

func TestLoadGatingTimesOutWithLoadReason(t *testing.T) {
	m, _ := loadManager(t, func() (float64, error) { return 8, nil })
	m.LoadWait = time.Minute
	_, err := m.Acquire(context.Background(), Request{Weight: 1, Check: "bench", Wait: 60 * time.Millisecond})
	var busy *BusyError
	require.ErrorAs(t, err, &busy)
	require.Equal(t, WaitingForLoad, busy.WaitingFor)
}

func TestLoadUnsupportedNeverBlocks(t *testing.T) {
	m, warnings := loadManager(t, func() (float64, error) { return 0, ErrLoadUnsupported })
	m.LoadWait = time.Hour
	start := time.Now()
	held, err := m.Acquire(context.Background(), Request{Weight: 1, Check: "bench", Warn: func(w string) { *warnings = append(*warnings, w) }})
	require.NoError(t, err)
	require.Less(t, time.Since(start), 5*time.Second)
	require.Len(t, *warnings, 1)
	require.Contains(t, (*warnings)[0], "load gating skipped")
	status, err := m.Snapshot()
	require.NoError(t, err)
	require.False(t, status.Load.Supported)
	require.NoError(t, held.Release())
}

func TestLoadAverageReadsOnSupportedPlatforms(t *testing.T) {
	value, err := LoadAverage()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		require.ErrorIs(t, err, ErrLoadUnsupported)
		return
	}
	require.NoError(t, err)
	require.GreaterOrEqual(t, value, 0.0)
}

func TestResolveSettings(t *testing.T) {
	env := map[string]string{}
	getenv := func(key string) string { return env[key] }

	settings, err := ResolveSettings(config.GlobalChecksConfig{}, getenv)
	require.NoError(t, err)
	require.Equal(t, DefaultCapacity(), settings.Capacity)
	require.GreaterOrEqual(t, settings.Capacity, 2, "default capacity is max(2, CPUs/4)")
	require.Zero(t, settings.MaxLoad, "load gating is opt-in")

	settings, err = ResolveSettings(config.GlobalChecksConfig{Slots: 3, MaxLoad: 6, LoadWait: "30s"}, getenv)
	require.NoError(t, err)
	require.Equal(t, Settings{Capacity: 3, MaxLoad: 6, LoadWait: 30 * time.Second}, settings)

	settings, err = ResolveSettings(config.GlobalChecksConfig{MaxLoad: 6}, getenv)
	require.NoError(t, err)
	require.Equal(t, DefaultLoadWait, settings.LoadWait)

	env[EnvSlots], env[EnvMaxLoad], env[EnvLoadWait] = "2", "1.5", "5s"
	settings, err = ResolveSettings(config.GlobalChecksConfig{Slots: 3, MaxLoad: 6, LoadWait: "30s"}, getenv)
	require.NoError(t, err)
	require.Equal(t, Settings{Capacity: 2, MaxLoad: 1.5, LoadWait: 5 * time.Second}, settings)

	for _, bad := range []map[string]string{{EnvSlots: "0"}, {EnvSlots: "x"}, {EnvMaxLoad: "-1"}, {EnvLoadWait: "soon"}} {
		env = bad
		_, err := ResolveSettings(config.GlobalChecksConfig{}, getenv)
		require.Error(t, err, "%v", bad)
	}
	env = map[string]string{}
	_, err = ResolveSettings(config.GlobalChecksConfig{Slots: -1}, getenv)
	require.Error(t, err)
	_, err = ResolveSettings(config.GlobalChecksConfig{LoadWait: "-5s"}, getenv)
	require.Error(t, err)
}
