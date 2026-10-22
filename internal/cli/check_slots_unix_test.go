//go:build !windows

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/lockfile"
)

// slotFixture prepares worktrees of one project whose expensive checks
// write traces outside every worktree.
func slotFixture(t *testing.T, capacity int, checksYAML string, worktrees ...string) (*agentFixture, string, map[string]string) {
	t.Helper()
	trace := evalSymlinks(t.TempDir())
	f := newAgentFixture(t, strings.ReplaceAll(checksYAML, "TRACE", trace), nil)
	f.env = append(f.env, "ANVIL_CHECK_SLOTS="+strconv.Itoa(capacity))
	paths := map[string]string{}
	for _, name := range worktrees {
		_, stderr, code := f.runJSON(f.repo, "ensure", "feature/"+name)
		require.Equal(t, 0, code, stderr)
		paths[name] = f.worktreePath("feature-" + name)
	}
	return f, trace, paths
}

func (f *agentFixture) start(dir string, args ...string) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer) {
	f.t.Helper()
	cmd := exec.Command(f.bin, args...)
	cmd.Dir, cmd.Env = dir, f.env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	require.NoError(f.t, cmd.Start())
	return cmd, &stdout, &stderr
}

func (f *agentFixture) slots() map[string]any {
	f.t.Helper()
	envelope, stderr, code := f.runJSON(f.repo, "slots")
	require.Equal(f.t, 0, code, stderr)
	require.Equal(f.t, false, envelope["mutating"])
	return envelope["data"].(map[string]any)
}

func (f *agentFixture) queued() []any {
	f.t.Helper()
	return f.slots()["queue"].([]any)
}

func waitFile(t *testing.T, path string) {
	t.Helper()
	require.Eventually(t, func() bool { _, err := os.Stat(path); return err == nil }, 20*time.Second, 20*time.Millisecond, path)
}

const concurrencyYAML = `checks:
  heavy:
    expensive: true
    commands:
      - mkdir -p "TRACE/running" && touch "TRACE/running/$ANVIL_CHECK_RUN_ID" && ls "TRACE/running" | wc -l >> "TRACE/samples" && sleep 1 && ls "TRACE/running" | wc -l >> "TRACE/samples" && rm "TRACE/running/$ANVIL_CHECK_RUN_ID"
`

// TestCheckSlotsCapConcurrencyAcrossWorktrees: four concurrent anvil
// processes in four worktrees never exceed the machine-wide capacity.
func TestCheckSlotsCapConcurrencyAcrossWorktrees(t *testing.T) {
	f, trace, wts := slotFixture(t, 2, concurrencyYAML, "a", "b", "c", "d")
	type outcome struct {
		code   int
		stderr string
		queued float64
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var outcomes []outcome
	for _, name := range []string{"a", "b", "c", "d"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(f.bin, "check", "heavy", "--allow-expensive", "--json")
			cmd.Dir, cmd.Env = wts[name], f.env
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			code := 0
			if exitErr, ok := err.(*exec.ExitError); ok {
				code = exitErr.ExitCode()
			} else if err != nil {
				code = -1
			}
			var envelope map[string]any
			queued := -1.0
			if json.Unmarshal(stdout.Bytes(), &envelope) == nil {
				if results, ok := field(t, envelope, "data", "results").([]any); ok && len(results) == 1 {
					queued, _ = field(t, results[0], "queued_ms").(float64)
				}
			}
			mu.Lock()
			outcomes = append(outcomes, outcome{code, stderr.String(), queued})
			mu.Unlock()
		}()
	}
	wg.Wait()
	waited, queuedMS := false, 0.0
	for _, o := range outcomes {
		require.Equal(t, 0, o.code, o.stderr)
		require.GreaterOrEqual(t, o.queued, 0.0, "every result reports queued_ms")
		waited = waited || strings.Contains(o.stderr, "waiting for slot (position")
		queuedMS = max(queuedMS, o.queued)
	}
	require.True(t, waited, "at least one check reports its queue position on stderr")
	require.Greater(t, queuedMS, 500.0, "queued runs record their wait")
	content, err := os.ReadFile(filepath.Join(trace, "samples"))
	require.NoError(t, err)
	samples := strings.Fields(string(content))
	require.Len(t, samples, 8)
	peak := 0
	for _, sample := range samples {
		n, err := strconv.Atoi(sample)
		require.NoError(t, err)
		peak = max(peak, n)
	}
	require.Equal(t, 2, peak, "capacity 2 is used but never exceeded: %v", samples)
	require.Empty(t, f.queued())
	require.Empty(t, f.slots()["holders"])
}

const holdYAML = `checks:
  hold:
    expensive: true
    commands:
      - echo $$ > "TRACE/$(basename "$PWD").pid"; touch "TRACE/$(basename "$PWD").started"; while [ ! -e "TRACE/release" ]; do sleep 0.05; done
`

// TestCheckSlotKilledHolderAndWaiterRecover: SIGKILL of a holder frees its
// slot once the supervised command is gone; a killed waiter's ticket is
// discarded instead of blocking the waiters behind it.
func TestCheckSlotKilledHolderAndWaiterRecover(t *testing.T) {
	f, trace, wts := slotFixture(t, 1, holdYAML, "a", "b", "c")
	holder, _, _ := f.start(wts["a"], "check", "hold", "--allow-expensive", "--json")
	waitFile(t, filepath.Join(trace, "feature-a.started"))

	data := f.slots()
	require.Equal(t, float64(1), data["capacity"])
	require.Equal(t, float64(1), data["in_use"])
	require.Equal(t, "hold", field(t, data, "holders", 0, "check"))
	require.Equal(t, wts["a"], field(t, data, "holders", 0, "worktree"))
	require.Equal(t, float64(holder.Process.Pid), field(t, data, "holders", 0, "pid"))

	waiter, _, _ := f.start(wts["b"], "check", "hold", "--allow-expensive", "--json", "--priority", "5")
	require.Eventually(t, func() bool { return len(f.queued()) == 1 }, 20*time.Second, 20*time.Millisecond)
	entry := f.queued()[0]
	require.Equal(t, "slot", field(t, entry, "waiting_for"))
	require.Equal(t, float64(1), field(t, entry, "position"))
	require.Equal(t, float64(5), field(t, entry, "priority"))
	require.Equal(t, wts["b"], field(t, entry, "worktree"))

	// status --json reports the machine capacity and this worktree's share.
	status, stderr, code := f.runJSON(wts["a"], "status")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, float64(1), field(t, status, "data", "slots", "capacity"))
	require.Equal(t, float64(1), field(t, status, "data", "slots", "in_use"))
	require.Equal(t, "hold", field(t, status, "data", "slots", "holders", 0, "check"))
	require.Empty(t, field(t, status, "data", "slots", "queue"))
	status, stderr, code = f.runJSON(wts["b"], "status")
	require.Equal(t, 0, code, stderr)
	require.Empty(t, field(t, status, "data", "slots", "holders"))
	require.Equal(t, "slot", field(t, status, "data", "slots", "queue", 0, "waiting_for"))
	require.Equal(t, float64(1), field(t, status, "data", "slots", "queue", 0, "position"))
	require.NoError(t, waiter.Process.Kill())
	require.Error(t, waiter.Wait())

	late, lateOut, lateErr := f.start(wts["c"], "check", "hold", "--allow-expensive", "--json")
	require.Eventually(t, func() bool {
		queue := f.queued()
		return len(queue) == 1 && field(t, queue[0], "worktree") == wts["c"]
	}, 20*time.Second, 20*time.Millisecond, "the killed waiter is no longer reported")

	require.NoError(t, holder.Process.Kill())
	require.Error(t, holder.Wait())
	waitFile(t, filepath.Join(trace, "feature-c.started"))
	content, err := os.ReadFile(filepath.Join(trace, "feature-a.pid"))
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	require.NoError(t, err)
	require.ErrorIs(t, syscall.Kill(pid, 0), syscall.ESRCH, "the killed holder's command was gone before its slot was reused")
	require.NoFileExists(t, filepath.Join(trace, "feature-b.started"))
	require.NoError(t, os.WriteFile(filepath.Join(trace, "release"), nil, 0o600))
	require.NoError(t, late.Wait(), "stdout: %s\nstderr: %s", lateOut, lateErr)
	require.Empty(t, f.queued())
}

const gatedYAML = `checks:
  bench:
    expensive: true
    reusable: true
    commands: ["echo run >> bench.count"]
  huge:
    slots: 3
    commands: ["true"]
  fast:
    commands: ["true"]
`

func TestCheckSlotWaitReuseCapacityAndInterrupt(t *testing.T) {
	f, _, wts := slotFixture(t, 1, gatedYAML, "a")
	wt := wts["a"]

	envelope, _, code := f.runJSON(wt, "check", "--list")
	require.Equal(t, 0, code)
	require.Equal(t, float64(1), field(t, checkEntry(t, envelope, "bench"), "slots", "weight"))
	require.Equal(t, float64(1), field(t, checkEntry(t, envelope, "bench"), "slots", "capacity"))
	require.Equal(t, float64(3), field(t, checkEntry(t, envelope, "huge"), "slots", "weight"))
	require.Nil(t, checkEntry(t, envelope, "fast")["slots"], "ungated checks have no slot info")

	_, stderr, code := f.runJSON(wt, "check", "bench", "--allow-expensive")
	require.Equal(t, 0, code, stderr)

	// Weight above capacity can never be admitted.
	envelope, _, code = f.runJSON(wt, "check", "huge")
	require.Equal(t, 5, code)
	require.Equal(t, "config_invalid", field(t, envelope, "error", "code"))
	require.Contains(t, field(t, envelope, "error", "message"), "needs 3 slots")

	envelope, _, code = f.runJSON(wt, "check", "bench", "--allow-expensive", "--slot-wait=-1s")
	require.Equal(t, 2, code)
	require.Equal(t, "invalid_arguments", field(t, envelope, "error", "code"))

	// Another holder occupies the only slot.
	slot, err := lockfile.Acquire(filepath.Join(f.runtimeRoot(), "slots", "slot-0.lock"))
	require.NoError(t, err)
	defer func() { require.NoError(t, slot.Release()) }()

	envelope, _, code = f.runJSON(wt, "check", "--list")
	require.Equal(t, 0, code)
	require.Equal(t, float64(1), field(t, checkEntry(t, envelope, "bench"), "slots", "in_use"))

	start := time.Now()
	envelope, stderr, code = f.runJSON(wt, "check", "bench", "--allow-expensive", "--slot-wait", "300ms")
	require.Equal(t, 7, code, stderr)
	require.GreaterOrEqual(t, time.Since(start), 300*time.Millisecond)
	require.Equal(t, "resource_busy", field(t, envelope, "error", "code"))
	details := field(t, envelope, "error", "details").(map[string]any)
	require.Equal(t, "slot", details["waiting_for"])
	require.Equal(t, float64(1), details["position"])
	require.Equal(t, float64(1), details["queued"])
	require.Len(t, details["holders"], 1)
	require.Equal(t, false, details["queue_busy"])
	require.Contains(t, stderr, "waiting for slot (position 1 of 1)")
	require.Equal(t, 1, countRuns(t, filepath.Join(wt, "bench.count")), "nothing ran")
	require.Empty(t, f.queued(), "the timed-out ticket is removed")

	// A reuse hit executes nothing and needs no slot.
	envelope, stderr, code = f.runJSON(wt, "check", "bench", "--allow-expensive", "--reuse", "--slot-wait", "300ms")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, true, field(t, envelope, "data", "results", 0, "reused"))

	// Interrupting a queued check removes its ticket: exit 130, no result.
	cmd, stdout, stderrBuf := f.start(wt, "check", "bench", "--allow-expensive", "--json")
	require.Eventually(t, func() bool { return len(f.queued()) == 1 }, 20*time.Second, 20*time.Millisecond)
	require.NoError(t, cmd.Process.Signal(syscall.SIGINT))
	err = cmd.Wait()
	exitErr, ok := err.(*exec.ExitError)
	require.True(t, ok, "%v", err)
	require.Equal(t, 130, exitErr.ExitCode(), stderrBuf.String())
	var interrupted map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &interrupted))
	require.Equal(t, "check_failed", field(t, interrupted, "error", "code"))
	require.Contains(t, fmt.Sprint(field(t, interrupted, "error", "message")), "interrupted before execution")
	require.Empty(t, field(t, interrupted, "data", "results"))
	require.Empty(t, f.queued())
	require.Equal(t, 1, countRuns(t, filepath.Join(wt, "bench.count")))
}

func TestCheckSlotTimeoutWithBusyQueueReportsHeldCapacity(t *testing.T) {
	f, _, wts := slotFixture(t, 1, gatedYAML, "a")
	wt := wts["a"]
	slot, err := lockfile.Acquire(filepath.Join(f.runtimeRoot(), "slots", "slot-0.lock"))
	require.NoError(t, err)
	defer func() { require.NoError(t, slot.Release()) }()
	queue, err := lockfile.Acquire(filepath.Join(f.runtimeRoot(), "slots", "queue.lock"))
	require.NoError(t, err)
	defer func() { require.NoError(t, queue.Release()) }()

	start := time.Now()
	envelope, _, code := f.runJSON(wt, "check", "bench", "--allow-expensive", "--slot-wait", "50ms")
	require.Equal(t, 7, code)
	require.Less(t, time.Since(start), 500*time.Millisecond, "queue lock contention stays inside the requested wait")
	require.Equal(t, "resource_busy", field(t, envelope, "error", "code"))
	details := field(t, envelope, "error", "details").(map[string]any)
	require.Equal(t, true, details["queue_busy"])
	require.Equal(t, float64(1), details["in_use"])
	require.Nil(t, details["holders"])

	list, _, code := f.runJSON(wt, "check", "--list")
	require.Equal(t, 0, code)
	listSlots := field(t, checkEntry(t, list, "bench"), "slots").(map[string]any)
	require.Equal(t, true, listSlots["queue_busy"])
	require.Nil(t, listSlots["queued"])
	require.Nil(t, listSlots["holders"])
	require.Nil(t, listSlots["queue"])

	status, _, code := f.runJSON(wt, "status")
	require.Equal(t, 0, code)
	require.Equal(t, true, field(t, status, "data", "slots", "queue_busy"))
	require.Nil(t, field(t, status, "data", "slots", "queued"))
	require.Nil(t, field(t, status, "data", "slots", "holders"))
	require.Nil(t, field(t, status, "data", "slots", "queue"))
}

func TestSlotsCommandReportsLoadAndCapacity(t *testing.T) {
	f := newAgentFixture(t, "", nil)
	f.env = append(f.env, "ANVIL_CHECK_SLOTS=3", "ANVIL_CHECK_MAX_LOAD=1000")
	data := f.slots()
	require.Equal(t, float64(3), data["capacity"])
	require.Equal(t, float64(0), data["in_use"])
	require.Equal(t, float64(1000), field(t, data, "load", "max"))
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		require.Equal(t, true, field(t, data, "load", "supported"), "Linux and macOS read the load average")
	}
	require.Empty(t, data["holders"])
	require.Empty(t, data["queue"])
	require.NoDirExists(t, filepath.Join(f.runtimeRoot(), "slots"), "a read-only view creates nothing")

	f.env = append(f.env, "ANVIL_CHECK_SLOTS=zero")
	envelope, _, code := f.runJSON(f.repo, "slots")
	require.Equal(t, 5, code)
	require.Equal(t, "config_invalid", field(t, envelope, "error", "code"))
}

const nestedYAML = `checks:
  outer:
    expensive: true
    commands:
      - BIN check inner --allow-expensive --json > TRACE/unbounded.json; echo $? > TRACE/unbounded.code; true
      - BIN check inner --allow-expensive --json --slot-wait 300ms > TRACE/bounded.json; echo $? > TRACE/bounded.code; true
  inner:
    expensive: true
    commands: ["true"]
`

// TestNestedGatedCheckFailsFastInsteadOfDeadlocking: a gated check started
// from a command of a check that holds a slot would wait forever at full
// capacity; with the default unbounded --slot-wait it is refused.
func TestNestedGatedCheckFailsFastInsteadOfDeadlocking(t *testing.T) {
	f, trace, wts := slotFixture(t, 1, strings.ReplaceAll(nestedYAML, "BIN", getAnvilBinary(t)), "a")
	cmd := exec.Command(f.bin, "check", "outer", "--allow-expensive", "--json")
	cmd.Dir, cmd.Env = wts["a"], f.env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Start())
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		require.NoError(t, err, stderr.String())
	case <-time.After(30 * time.Second):
		require.NoError(t, cmd.Process.Kill())
		<-done
		t.Fatal("nested gated check deadlocked")
	}
	readCode := func(name string) string {
		content, err := os.ReadFile(filepath.Join(trace, name+".code"))
		require.NoError(t, err)
		return strings.TrimSpace(string(content))
	}
	readError := func(name string) map[string]any {
		var envelope map[string]any
		content, err := os.ReadFile(filepath.Join(trace, name+".json"))
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(content, &envelope))
		return envelope["error"].(map[string]any)
	}
	require.Equal(t, "5", readCode("unbounded"))
	unbounded := readError("unbounded")
	require.Equal(t, "config_invalid", unbounded["code"])
	require.Contains(t, unbounded["message"], "inside check \"outer\"")
	require.Equal(t, "7", readCode("bounded"), "an explicit bound keeps queueing behavior")
	require.Equal(t, "resource_busy", readError("bounded")["code"])
}
