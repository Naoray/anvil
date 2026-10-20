//go:build linux || darwin

package procs

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func startSpec(t *testing.T, command string) Started {
	t.Helper()
	dir := t.TempDir()
	started, err := Start(Spec{Command: command, Dir: dir, Env: os.Environ(), LogPath: filepath.Join(dir, "log")})
	require.NoError(t, err)
	t.Cleanup(func() {
		if Check(started.PID, started.Identity) == StateRunning || groupExists(started.PID) {
			if err := unix.Kill(-started.PID, unix.SIGKILL); err != nil {
				t.Logf("cleanup kill: %v", err)
			}
		}
	})
	return started
}

func TestStartIdentityStableAndDistinct(t *testing.T) {
	self, err := StartIdentity(os.Getpid())
	require.NoError(t, err)
	again, err := StartIdentity(os.Getpid())
	require.NoError(t, err)
	require.Equal(t, self, again)
	parent, err := StartIdentity(os.Getppid())
	require.NoError(t, err)
	require.NotEqual(t, self, parent)
	_, err = StartIdentity(0)
	require.ErrorIs(t, err, ErrNoProcess)
}

func TestStartAndStopVerifiedProcess(t *testing.T) {
	started := startSpec(t, "echo hello; exec sleep 30")
	require.Equal(t, StateRunning, Check(started.PID, started.Identity))
	result, err := Stop(started.PID, started.Identity, 5*time.Second)
	require.NoError(t, err)
	require.Equal(t, StopTerminated, result)
	require.Eventually(t, func() bool { return Check(started.PID, started.Identity) == StateExited }, 5*time.Second, 20*time.Millisecond)
	result, err = Stop(started.PID, started.Identity, time.Second)
	require.NoError(t, err)
	require.Equal(t, StopAlreadyExited, result)
}

func TestStopEscalatesToKillWhenTermIgnored(t *testing.T) {
	started := startSpec(t, "trap '' TERM; while true; do sleep 1; done")
	time.Sleep(200 * time.Millisecond) // let the shell install its trap
	result, err := Stop(started.PID, started.Identity, 300*time.Millisecond)
	require.NoError(t, err)
	require.Equal(t, StopKilled, result)
}

// TestStopNeverSignalsRecycledPID models PID reuse: the PID belongs to a live
// process whose start identity differs from the record.
func TestStopNeverSignalsRecycledPID(t *testing.T) {
	victim := startSpec(t, "exec sleep 30")
	result, err := Stop(victim.PID, victim.Identity+"-different-start", time.Second)
	require.NoError(t, err)
	require.Equal(t, StopNotOwned, result)
	require.Equal(t, StateReused, Check(victim.PID, victim.Identity+"-different-start"))
	require.Equal(t, StateRunning, Check(victim.PID, victim.Identity), "unrelated process untouched")
}

func TestStopTerminatesOrphanedGroupAfterLeaderExit(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	started := startSpec(t, "sleep 30 & echo $! > "+pidFile+"; exit 0")
	var childPID int
	require.Eventually(t, func() bool {
		content, err := os.ReadFile(pidFile)
		if err != nil || len(strings.TrimSpace(string(content))) == 0 {
			return false
		}
		childPID, err = strconv.Atoi(strings.TrimSpace(string(content)))
		return err == nil
	}, 5*time.Second, 20*time.Millisecond)
	require.Eventually(t, func() bool { return Check(started.PID, started.Identity) == StateExited }, 5*time.Second, 20*time.Millisecond)
	require.True(t, groupExists(started.PID))

	result, err := Stop(started.PID, started.Identity, 5*time.Second)
	require.NoError(t, err)
	require.Equal(t, StopTerminated, result)
	require.Eventually(t, func() bool {
		_, err := StartIdentity(childPID)
		return err != nil
	}, 5*time.Second, 20*time.Millisecond)
}
