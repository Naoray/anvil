//go:build linux || darwin

package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

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
