// Check commands use sh and lock inheritance is POSIX-only.

//go:build !windows

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func setFixtureEnv(f *agentFixture, key, value string) {
	f.env = subprocessEnvFrom(append(f.env, key+"="+value))
}

func TestCheckReuseFollowsTheEnvironmentCommandsReceive(t *testing.T) {
	yaml := `components:
  - name: app
    path: app
    env:
      MODE: ${PR27_MODE}
checks:
  component:
    reusable: true
    commands:
      - run: test "$MODE" = pass
        component: app
  command:
    reusable: true
    commands:
      - run: test "$CMD_MODE" = pass
        env: {CMD_MODE: "${PR27_MODE}"}
  inherited:
    reusable: true
    commands: ['test "$PR27_MODE" = pass']
`
	f := newAgentFixture(t, yaml, map[string]string{"app/.keep": ""})
	setFixtureEnv(f, "PR27_MODE", "pass")
	_, stderr, code := f.run(f.repo, "ensure", "feature/x", "--skip-prepare")
	require.Equal(t, 0, code, stderr)
	wt := f.worktreePath("feature-x")
	for _, name := range []string{"component", "command", "inherited"} {
		_, stderr, code := f.runJSON(wt, "check", name)
		require.Equal(t, 0, code, name+": "+stderr)
	}
	setFixtureEnv(f, "PR27_MODE", "fail")
	for _, name := range []string{"component", "command", "inherited"} {
		envelope, stderr, code := f.runJSON(wt, "check", name, "--reuse")
		require.NotEqual(t, 0, code, name+": a changed environment must not reuse the passing run\n"+stderr)
		require.Equal(t, false, field(t, envelope, "data", "results", 0, "reused"), name)
	}
}

const lockedCheckYAML = `checks:
  hold:
    locks: [gpu]
    commands: ['trap "" TERM; echo $$ > "$HOLD_PID"; exec sleep 30']
  other:
    locks: [gpu]
    commands: ["touch other.ran"]
`

func waitForPID(t *testing.T, path string) int {
	t.Helper()
	var pid int
	require.Eventually(t, func() bool {
		content, err := os.ReadFile(path)
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(content)))
		return err == nil && pid > 0
	}, 10*time.Second, 20*time.Millisecond)
	return pid
}

func TestKilledAnvilKeepsLocksWhileTheCommandSurvives(t *testing.T) {
	f := newAgentFixture(t, lockedCheckYAML, nil)
	pidPath := filepath.Join(t.TempDir(), "hold.pid")
	setFixtureEnv(f, "HOLD_PID", pidPath)
	_, stderr, code := f.run(f.repo, "ensure", "feature/x", "--skip-prepare")
	require.Equal(t, 0, code, stderr)
	wt := f.worktreePath("feature-x")

	supervisor := exec.Command(f.bin, "check", "hold", "--quiet")
	supervisor.Dir, supervisor.Env = wt, f.env
	require.NoError(t, supervisor.Start())
	pid := waitForPID(t, pidPath)
	defer func() { _ = syscall.Kill(-pid, syscall.SIGKILL) }()
	require.NoError(t, supervisor.Process.Kill())
	require.Error(t, supervisor.Wait())
	require.NoError(t, syscall.Kill(pid, 0), "the command survived its supervisor")

	envelope, stderr, code := f.runJSON(wt, "check", "other", "--lock-wait", "0s")
	require.Equal(t, "resource_busy", field(t, envelope, "error", "code"), stderr)
	require.NotEqual(t, 0, code)
	require.NoFileExists(t, filepath.Join(wt, "other.ran"), "no overlapping resource user")

	_, stderr, code = f.run(f.repo, "remove", "feature-x", "--force")
	require.Equal(t, 7, code, stderr)
	require.DirExists(t, wt, "cleanup refuses while the command lives")

	require.NoError(t, syscall.Kill(-pid, syscall.SIGKILL))
	require.Eventually(t, func() bool {
		_, _, code := f.run(wt, "check", "other", "--lock-wait", "0s")
		return code == 0
	}, 10*time.Second, 100*time.Millisecond, "the lock is free once the command is gone")
}

func TestCheckCannotStartDuringRemoval(t *testing.T) {
	marks := t.TempDir()
	yaml := `cleanup:
  steps:
    - name: bash.run
      condition:
        command: touch ` + filepath.Join(marks, "cleanup.started") + `; sleep 3
checks:
  fast:
    commands: ["touch ` + filepath.Join(marks, "check.ran") + `"]
`
	f := newAgentFixture(t, yaml, nil)
	_, stderr, code := f.run(f.repo, "ensure", "feature/x", "--skip-prepare")
	require.Equal(t, 0, code, stderr)
	wt := f.worktreePath("feature-x")

	removal := exec.Command(f.bin, "remove", "feature-x", "--force")
	removal.Dir, removal.Env = f.repo, f.env
	var removalOutput strings.Builder
	removal.Stdout, removal.Stderr = &removalOutput, &removalOutput
	require.NoError(t, removal.Start())
	defer func() {
		if t.Failed() {
			t.Log(removalOutput.String())
		}
	}()
	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(marks, "cleanup.started"))
		return err == nil
	}, 10*time.Second, 20*time.Millisecond)

	_, stderr, code = f.run(wt, "check", "fast")
	require.NotEqual(t, 0, code, stderr)
	require.NoError(t, removal.Wait(), "removal completes")
	require.NoFileExists(t, filepath.Join(marks, "check.ran"), "no check ran during or after removal")
	require.NoDirExists(t, wt)
}
