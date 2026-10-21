package checks

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
)

func TestProbeDeadlineKillsOutputHoldingGrandchild(t *testing.T) {
	f := newFixture(t)
	pidFile := filepath.Join(t.TempDir(), "pid")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := ComputeIdentity(ctx, IdentityRequest{WorktreePath: f.worktree, RuntimeDir: f.runtime, Env: f.env(), Check: config.CheckConfig{Name: "probe", Toolchain: []string{"sleep 30 & echo $! > '" + pidFile + "'; wait"}, Commands: cmds("true")}})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(start), 2*time.Second)
	pid, err := os.ReadFile(pidFile)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return exec.Command("kill", "-0", strings.TrimSpace(string(pid))).Run() != nil }, time.Second, 10*time.Millisecond)
}
