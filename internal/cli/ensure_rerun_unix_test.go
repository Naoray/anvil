//go:build !windows

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEnsureRepairsKilledRefresh(t *testing.T) {
	f := newAgentFixture(t, rerunFixtureYAML, map[string]string{"deps.lock": "v1"})
	_, stderr, code := f.runJSON(f.repo, "ensure")
	require.Equal(t, 0, code, stderr)
	gate := filepath.Join(f.repo, "interrupt-rerun")
	require.NoError(t, os.WriteFile(gate, nil, 0o644))
	cmd := exec.Command(f.bin, "ensure", "--refresh", "--json")
	cmd.Dir, cmd.Env = f.repo, f.env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			require.NoError(t, syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL))
			require.Error(t, cmd.Wait())
		}
	})
	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(f.repo, "entered"))
		return err == nil
	}, 15*time.Second, 20*time.Millisecond)
	require.NoError(t, syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL))
	require.Error(t, cmd.Wait())
	require.NoError(t, os.Remove(gate))
	product, err := os.ReadFile(filepath.Join(f.repo, "product"))
	require.NoError(t, err)
	require.Equal(t, "broken\n", string(product))
	envelope, stderr, code := f.runJSON(f.repo, "ensure")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, []string{"ran"}, stepResults(t, envelope))
	product, err = os.ReadFile(filepath.Join(f.repo, "product"))
	require.NoError(t, err)
	require.Equal(t, "good\n", string(product))
	require.Equal(t, true, field(t, envelope, "data", "prepare", "ready"))
}
