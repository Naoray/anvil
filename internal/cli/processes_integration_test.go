//go:build linux || darwin

package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v3"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/procs"
)

const longProcessYAML = `processes:
  - name: web
    command: exec sleep 60
    stop_timeout: 2s
`

// killGroupsOnCleanup SIGKILLs every process group in pids when the test
// ends, so a failing assertion never leaks sleepers.
func killGroupsOnCleanup(t *testing.T, mu *sync.Mutex, pids *[]int) {
	t.Helper()
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		for _, pid := range *pids {
			if err := unix.Kill(-pid, unix.SIGKILL); err != nil && !errors.Is(err, unix.ESRCH) {
				t.Logf("cleanup kill %d: %v", pid, err)
			}
		}
	})
}

// startJSON runs `anvil start --json` without testing.T so it is safe to call
// from goroutines.
func (f *agentFixture) startJSON(wt string) (map[string]any, error) {
	cmd := exec.Command(f.bin, "start", wt, "--json")
	cmd.Dir = f.repo
	cmd.Env = f.env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, errors.Join(err, errors.New(stderr.String()))
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		return nil, err
	}
	return envelope, nil
}

// TestConcurrentStartLaunchesOnce reproduces review finding 4910: parallel
// starts of the same worktree must launch and record exactly one process.
func TestConcurrentStartLaunchesOnce(t *testing.T) {
	f := newAgentFixture(t, longProcessYAML, nil)
	f.env = subprocessEnvFrom(append(f.env, "ANVIL_PORT_RANGE=43200-43299"))
	_, stderr, code := f.runJSON(f.repo, "ensure", "feature/a")
	require.Equal(t, 0, code, stderr)

	var mu sync.Mutex
	var pids []int
	killGroupsOnCleanup(t, &mu, &pids)

	const parallel = 4
	results := make([]string, parallel)
	errs := make([]error, parallel)
	var wg sync.WaitGroup
	for i := 0; i < parallel; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			envelope, err := f.startJSON("feature-a")
			if err != nil {
				errs[i] = err
				return
			}
			process := envelope["data"].(map[string]any)["processes"].([]any)[0].(map[string]any)
			results[i] = process["result"].(string)
			mu.Lock()
			pids = append(pids, int(process["pid"].(float64)))
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}

	started := 0
	for _, result := range results {
		if result == "started" {
			started++
		} else {
			require.Equal(t, "already_running", result)
		}
	}
	require.Equal(t, 1, started, "exactly one start launches: %v", results)

	envelope, stderr, code := f.runJSON(f.repo, "stop", "feature-a")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, "terminated", field(t, envelope, "data", "processes", 0, "result"))
	mu.Lock()
	defer mu.Unlock()
	for _, pid := range pids {
		require.Eventually(t, func() bool {
			_, err := procs.StartIdentity(pid)
			return errors.Is(err, procs.ErrNoProcess)
		}, 5*time.Second, 20*time.Millisecond, "pid %d survived stop", pid)
	}
}

const backgroundingProcessYAML = `processes:
  - name: web
    command: sleep 60 & exit 0
    stop_timeout: 2s
`

// TestRestartOfSelfBackgroundingCommandIsAlreadyRunning reproduces review
// finding 4912: a leader that exits at once while its own group keeps
// running must read as already running, not trigger a second launch.
func TestRestartOfSelfBackgroundingCommandIsAlreadyRunning(t *testing.T) {
	f := newAgentFixture(t, backgroundingProcessYAML, nil)
	f.env = subprocessEnvFrom(append(f.env, "ANVIL_PORT_RANGE=43300-43399"))
	_, stderr, code := f.runJSON(f.repo, "ensure", "feature/a")
	require.Equal(t, 0, code, stderr)

	var mu sync.Mutex
	var pids []int
	killGroupsOnCleanup(t, &mu, &pids)

	envelope, stderr, code := f.runJSON(f.repo, "start", "feature-a")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, "started", field(t, envelope, "data", "processes", 0, "result"))
	first := int(field(t, envelope, "data", "processes", 0, "pid").(float64))
	mu.Lock()
	pids = append(pids, first)
	mu.Unlock()
	// Let the command's shell return, leaving only its background sleep.
	time.Sleep(time.Second)

	envelope, stderr, code = f.runJSON(f.repo, "start", "feature-a")
	require.Equal(t, 0, code, stderr)
	second := int(field(t, envelope, "data", "processes", 0, "pid").(float64))
	mu.Lock()
	pids = append(pids, second)
	mu.Unlock()
	require.Equal(t, "already_running", field(t, envelope, "data", "processes", 0, "result"))
	require.Equal(t, first, second)

	envelope, stderr, code = f.runJSON(f.repo, "stop", "feature-a")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, "terminated", field(t, envelope, "data", "processes", 0, "result"))
	require.Eventually(t, func() bool {
		err := unix.Kill(-first, 0)
		return errors.Is(err, unix.ESRCH)
	}, 5*time.Second, 20*time.Millisecond, "background group stopped")
}

// TestStopNeverSignalsUnrelatedGroupOfExitedLeader reproduces review finding
// 4911: a record whose PID now names an unrelated process group with a dead
// leader must never be signaled, because nothing proves that group is ours.
func TestStopNeverSignalsUnrelatedGroupOfExitedLeader(t *testing.T) {
	f := newAgentFixture(t, longProcessYAML, nil)
	f.env = subprocessEnvFrom(append(f.env, "ANVIL_PORT_RANGE=43400-43499"))
	_, stderr, code := f.runJSON(f.repo, "ensure", "feature/a")
	require.Equal(t, 0, code, stderr)

	var mu sync.Mutex
	var pids []int
	killGroupsOnCleanup(t, &mu, &pids)

	envelope, stderr, code := f.runJSON(f.repo, "start", "feature-a")
	require.Equal(t, 0, code, stderr)
	ours := int(field(t, envelope, "data", "processes", 0, "pid").(float64))
	mu.Lock()
	pids = append(pids, ours)
	mu.Unlock()

	// An unrelated session whose leader exits, leaving its group alive.
	childFile := filepath.Join(t.TempDir(), "child.pid")
	victim := exec.Command("sh", "-c", "sleep 60 & echo $! > "+childFile+"; exit 0")
	victim.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	require.NoError(t, victim.Start())
	victimGroup := victim.Process.Pid
	mu.Lock()
	pids = append(pids, victimGroup)
	mu.Unlock()
	require.NoError(t, victim.Wait())
	content, err := os.ReadFile(childFile)
	require.NoError(t, err)
	var victimChild int
	_, err = fmt.Sscan(string(content), &victimChild)
	require.NoError(t, err)

	// The record now points at the victim's group with a dead leader.
	path := filepath.Join(f.worktreePath("feature-a"), config.LocalStateFile)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, yaml.Unmarshal(raw, &doc))
	record := doc["processes"].(map[string]any)["web"].(map[string]any)
	record["pid"] = victimGroup
	record["identity"] = ""
	raw, err = yaml.Marshal(doc)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0o644))

	envelope, stderr, code = f.runJSON(f.repo, "stop", "feature-a")
	require.Equal(t, 0, code, stderr)
	require.NotEqual(t, "terminated", field(t, envelope, "data", "processes", 0, "result"))
	require.NotEqual(t, "killed", field(t, envelope, "data", "processes", 0, "result"))
	time.Sleep(500 * time.Millisecond)
	_, err = procs.StartIdentity(victimChild)
	require.NoError(t, err, "unrelated group member must survive")
}
