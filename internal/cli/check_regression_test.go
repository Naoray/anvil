package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/naoray/anvil/internal/lockfile"
	"github.com/naoray/anvil/internal/wtstate"

	"github.com/stretchr/testify/require"
)

func TestCheckReuseFingerprintsEffectiveEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("checks require Unix supervision")
	}
	for _, scope := range []string{"component", "command", "inherited"} {
		t.Run(scope, func(t *testing.T) {
			yaml := "components:\n  - name: app\n    path: app\n    env: {MODE: '${CHECK_MODE}'}\nchecks:\n  env:\n    reusable: true\n    commands:\n"
			switch scope {
			case "component":
				yaml += "      - run: test \"$MODE\" = pass\n        component: app\n"
			case "command":
				yaml += "      - run: test \"$MODE\" = pass\n        env: {MODE: '${CHECK_MODE}'}\n"
			case "inherited":
				yaml += "      - test \"$CHECK_MODE\" = pass\n"
			}
			f := newAgentFixture(t, yaml, map[string]string{"app/.keep": ""})
			base := append([]string{}, f.env...)
			for i, mode := range []string{"pass", "pass", "fail"} {
				f.env = append(append([]string{}, base...), fmt.Sprintf("CHECK_MODE=%s", mode))
				envelope, stderr, code := f.runJSON(f.repo, "check", "env", "--reuse")
				if mode == "fail" {
					require.Equal(t, 1, code, stderr)
				} else {
					require.Equal(t, 0, code, stderr)
				}
				require.Equal(t, i == 1, field(t, envelope, "data", "results", 0, "reused"))
			}
		})
	}
}

func TestCheckRejectsSymlinkInputs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlinks")
	}
	for _, kind := range []string{"leaf", "ancestor", "nested", "inside"} {
		t.Run(kind, func(t *testing.T) {
			pattern := "fixture/input"
			if kind == "nested" {
				pattern = "fixture"
			}
			f := newAgentFixture(t, fmt.Sprintf("checks:\n  links:\n    reusable: true\n    inputs: [%q]\n    commands: ['true']\n", pattern), map[string]string{".gitignore": ".anvil.local\nfixture/\n", "inside.txt": "inside"})
			outside := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(outside, "input"), []byte("good"), 0o600))
			fixture := filepath.Join(f.repo, "fixture")
			if kind == "ancestor" {
				require.NoError(t, os.Symlink(outside, fixture))
			} else {
				require.NoError(t, os.MkdirAll(fixture, 0o700))
				target := filepath.Join(outside, "input")
				if kind == "inside" {
					target = filepath.Join(f.repo, "inside.txt")
				}
				require.NoError(t, os.Symlink(target, filepath.Join(fixture, "input")))
			}
			envelope, stderr, code := f.runJSON(f.repo, "check", "links", "--reuse")
			require.NotZero(t, code, stderr)
			require.Contains(t, field(t, envelope, "error", "message"), "symlink")
		})
	}
}

func TestCheckRejectsDirtyNestedRepositoryIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix checks")
	}
	f := newAgentFixture(t, "checks:\n  sub:\n    reusable: true\n    commands: ['true']\n", nil)
	sub := filepath.Join(f.repo, "sub")
	require.NoError(t, os.MkdirAll(sub, 0o700))
	gitRun(t, sub, "init", "-b", "main")
	gitRun(t, sub, "config", "user.email", "test@example.com")
	gitRun(t, sub, "config", "user.name", "Test")
	require.NoError(t, os.WriteFile(filepath.Join(sub, "src"), []byte("clean"), 0o600))
	gitRun(t, sub, "add", ".")
	gitRun(t, sub, "commit", "-m", "nested")
	gitRun(t, f.repo, "add", "sub")
	gitRun(t, f.repo, "commit", "-m", "gitlink")
	_, stderr, code := f.runJSON(f.repo, "check", "sub")
	require.Zero(t, code, stderr)
	for _, content := range []string{"dirty A", "dirty B"} {
		require.NoError(t, os.WriteFile(filepath.Join(sub, "src"), []byte(content), 0o600))
		envelope, stderr, code := f.runJSON(f.repo, "check", "sub", "--reuse")
		require.NotZero(t, code, stderr)
		require.Contains(t, field(t, envelope, "error", "message"), "dirty submodule or nested repository")
	}
}

func TestCheckRejectsDirectoryArtifacts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix checks")
	}
	f := newAgentFixture(t, `checks:
  bundle:
    reusable: true
    commands: ['mkdir -p "$ANVIL_ARTIFACTS_DIR/bundle"; echo good > "$ANVIL_ARTIFACTS_DIR/bundle/result"']
    artifacts: ['${ANVIL_ARTIFACTS_DIR}/bundle']
`, nil)
	for i := 0; i < 4; i++ {
		envelope, stderr, code := f.runJSON(f.repo, "check", "bundle", "--reuse")
		require.Equal(t, 1, code, stderr)
		result := field(t, envelope, "data", "results", 0)
		require.Equal(t, false, field(t, result, "reused"))
		require.Contains(t, field(t, result, "error"), "directory artifacts are unsupported")
		bundles, err := filepath.Glob(filepath.Join(f.runtimeRoot(), "worktrees", "*", "artifacts", "bundle"))
		require.NoError(t, err)
		require.Len(t, bundles, 1)
		switch i {
		case 0:
			require.NoError(t, os.WriteFile(filepath.Join(bundles[0], "result"), []byte("tampered"), 0o600))
		case 1:
			require.NoError(t, os.Remove(filepath.Join(bundles[0], "result")))
		case 2:
			require.NoError(t, os.WriteFile(filepath.Join(bundles[0], "added"), []byte("extra"), 0o600))
		}
	}
}

func TestCheckIdentityCapturedAfterAllResourceLocks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix checks")
	}
	f := newAgentFixture(t, `checks:
  wait:
    locks: [a, z]
    inputs: [input.count]
    commands: ['cat input.count > "$ANVIL_ARTIFACTS_DIR/consumed"']
`, map[string]string{"input.count": "A"})
	gate, err := lockfile.Acquire(filepath.Join(f.runtimeRoot(), "locks", "z.lock"))
	require.NoError(t, err)
	defer gate.Release()
	running := exec.Command(f.bin, "check", "wait", "--lock-wait", "30s", "--json")
	running.Dir, running.Env = f.repo, f.env
	var output bytes.Buffer
	running.Stdout, running.Stderr = &output, &output
	require.NoError(t, running.Start())
	t.Cleanup(func() {
		if running.ProcessState == nil {
			require.NoError(t, running.Process.Kill())
			require.Error(t, running.Wait())
		}
	})
	require.Eventually(t, func() bool {
		held, err := lockfile.IsHeld(filepath.Join(f.runtimeRoot(), "locks", "a.lock"))
		return err == nil && held
	}, 10*time.Second, 10*time.Millisecond)
	require.NoError(t, os.WriteFile(filepath.Join(f.repo, "input.count"), []byte("B"), 0o600))
	require.NoError(t, gate.Release())
	require.NoError(t, running.Wait(), output.String())
	envelope, stderr, code := f.runJSON(f.repo, "check", "--list")
	require.Zero(t, code, stderr)
	require.Equal(t, true, field(t, checkEntry(t, envelope, "wait"), "assessment", "passing_and_current"), output.String())
	require.NoError(t, os.WriteFile(filepath.Join(f.repo, "input.count"), []byte("A"), 0o600))
	envelope, _, _ = f.runJSON(f.repo, "check", "--list")
	require.Equal(t, false, field(t, checkEntry(t, envelope, "wait"), "assessment", "passing_and_current"))
}

func TestCleanupExcludesCheckStartup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FIFO synchronization")
	}
	for _, operation := range []string{"remove", "prune", "unlink"} {
		t.Run(operation, func(t *testing.T) {
			barrier := filepath.Join(t.TempDir(), "release")
			ready := barrier + ".ready"
			require.NoError(t, exec.Command("mkfifo", barrier).Run())
			yaml := fmt.Sprintf("cleanup:\n  steps:\n    - name: command.run\n      condition:\n        command: %q\nchecks:\n  start:\n    commands: ['touch check.count']\n", "touch '"+ready+"'; read answer < '"+barrier+"'")
			f := newAgentFixture(t, yaml, nil)
			if operation == "prune" {
				remote := filepath.Join(t.TempDir(), "origin.git")
				gitRun(t, f.repo, "init", "--bare", remote)
				gitRun(t, f.repo, "remote", "add", "origin", remote)
				gitRun(t, f.repo, "push", "origin", "main")
			}

			_, stderr, code := f.runJSON(f.repo, "ensure", "feature/gate")
			require.Zero(t, code, stderr)
			wt := f.worktreePath("feature-gate")
			args := []string{"remove", "feature-gate", "--force"}
			if operation == "prune" {
				args = []string{"prune", "--force"}
			}
			if operation == "unlink" {
				args = []string{"unlink", "demo", "--clean", "--force"}
			}
			cleanup := exec.Command(f.bin, args...)
			cleanup.Dir, cleanup.Env = f.repo, f.env
			var output bytes.Buffer
			cleanup.Stdout, cleanup.Stderr = &output, &output
			require.NoError(t, cleanup.Start())
			t.Cleanup(func() {
				if cleanup.ProcessState == nil {
					require.NoError(t, cleanup.Process.Kill())
					require.Error(t, cleanup.Wait())
				}
			})
			require.Eventually(t, func() bool { _, err := os.Stat(ready); return err == nil }, 15*time.Second, 10*time.Millisecond)
			envelope, stderr, code := f.runJSON(wt, "check", "start")
			// Unblock before assertions so a failing regression never leaks cleanup.
			release, err := os.OpenFile(barrier, os.O_WRONLY, 0o600)
			require.NoError(t, err)
			_, err = release.WriteString("continue\n")
			require.NoError(t, err)
			require.NoError(t, release.Close())
			require.NoError(t, cleanup.Wait(), output.String())
			require.Equal(t, 7, code, stderr)
			require.Equal(t, "cleanup_blocked", field(t, envelope, "error", "code"))
		})
	}
}

func TestCleanupRefusesPrepareLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix fixture")
	}
	f := newAgentFixture(t, "", nil)
	_, stderr, code := f.runJSON(f.repo, "ensure", "feature/preparing")
	require.Zero(t, code, stderr)
	wt := f.worktreePath("feature-preparing")
	lock, err := wtstate.AcquirePrepareLock(wt)
	require.NoError(t, err)
	defer lock.Release()
	stdout, stderr, code := f.run(f.repo, "remove", "feature-preparing", "--force")
	require.Equal(t, 7, code, stdout+stderr)
	require.Contains(t, stderr, "preparation")
	require.DirExists(t, wt)
}

func TestCheckCancellationWhileWaitingForResourceLocks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix signals")
	}
	f := newAgentFixture(t, "checks:\n  wait:\n    locks: [a, z]\n    commands: ['touch ran.count']\n", nil)
	gate, err := lockfile.Acquire(filepath.Join(f.runtimeRoot(), "locks", "z.lock"))
	require.NoError(t, err)
	defer gate.Release()
	running := exec.Command(f.bin, "check", "wait", "--lock-wait=-1s", "--json")
	running.Dir, running.Env = f.repo, f.env
	var output bytes.Buffer
	running.Stdout, running.Stderr = &output, &output
	require.NoError(t, running.Start())
	require.Eventually(t, func() bool {
		held, err := lockfile.IsHeld(filepath.Join(f.runtimeRoot(), "locks", "a.lock"))
		return err == nil && held
	}, 10*time.Second, 10*time.Millisecond)
	require.NoError(t, running.Process.Signal(os.Interrupt))
	done := make(chan error, 1)
	go func() { done <- running.Wait() }()
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(2 * time.Second):
		require.NoError(t, gate.Release())
		<-done
		t.Fatal("SIGINT did not interrupt resource-lock contention")
	}
	require.Equal(t, 130, running.ProcessState.ExitCode(), output.String())
	require.NoFileExists(t, filepath.Join(f.repo, "ran.count"))
	earlier, err := lockfile.TryAcquire(filepath.Join(f.runtimeRoot(), "locks", "a.lock"))
	require.NoError(t, err, "cancellation releases already acquired locks")
	require.NoError(t, earlier.Release())
}

func TestToolchainProbeCancellationKillsGrandchildren(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix signals")
	}
	for _, list := range []bool{false, true} {
		t.Run(fmt.Sprint("list=", list), func(t *testing.T) {
			f := newAgentFixture(t, `checks:
  probe:
    toolchain: ['if test -e block.count; then sleep 30 & echo $! > probe.pid.count; wait; else echo tool-v1; fi']
    commands: ['true']
`, nil)
			_, stderr, code := f.runJSON(f.repo, "check", "probe")
			require.Zero(t, code, stderr)
			require.NoError(t, os.WriteFile(filepath.Join(f.repo, "block.count"), nil, 0o600))
			args := []string{"check", "probe", "--json"}
			if list {
				args = append(args, "--list")
			}
			running := exec.Command(f.bin, args...)
			running.Dir, running.Env = f.repo, f.env
			var output bytes.Buffer
			running.Stdout, running.Stderr = &output, &output
			require.NoError(t, running.Start())
			pidFile := filepath.Join(f.repo, "probe.pid.count")
			require.Eventually(t, func() bool { _, err := os.Stat(pidFile); return err == nil }, 10*time.Second, 10*time.Millisecond)
			pid, err := os.ReadFile(pidFile)
			require.NoError(t, err)
			require.NoError(t, running.Process.Signal(os.Interrupt))
			done := make(chan error, 1)
			go func() { done <- running.Wait() }()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				require.NoError(t, exec.Command("kill", "-KILL", strings.TrimSpace(string(pid))).Run())
				<-done
				t.Error("probe cancellation waited for a surviving grandchild")
			}
			if exec.Command("kill", "-0", strings.TrimSpace(string(pid))).Run() == nil {
				require.NoError(t, exec.Command("kill", "-KILL", strings.TrimSpace(string(pid))).Run())
				t.Error("probe left a grandchild running")
			}
			require.Equal(t, 130, running.ProcessState.ExitCode(), output.String())
		})
	}
}

func TestKilledAnvilCannotReleaseLiveCommandResource(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix signals")
	}
	f := newAgentFixture(t, `checks:
  orphan:
    locks: [gpu]
    commands: ['trap "" TERM; echo $$ > orphan.pid.count; exec sleep 30']
  competitor:
    locks: [gpu]
    commands: ['if kill -0 "$(cat orphan.pid.count)" 2>/dev/null; then exit 91; fi']
`, nil)
	_, stderr, code := f.runJSON(f.repo, "ensure", "feature/orphan")
	require.Zero(t, code, stderr)
	wt := f.worktreePath("feature-orphan")
	running := exec.Command(f.bin, "check", "orphan", "--json")
	running.Dir, running.Env = wt, f.env
	require.NoError(t, running.Start())
	pidFile := filepath.Join(wt, "orphan.pid.count")
	require.Eventually(t, func() bool { _, err := os.Stat(pidFile); return err == nil }, 10*time.Second, 10*time.Millisecond)
	pid, err := os.ReadFile(pidFile)
	require.NoError(t, err)
	t.Cleanup(func() {
		if exec.Command("kill", "-0", strings.TrimSpace(string(pid))).Run() == nil {
			require.NoError(t, exec.Command("kill", "-KILL", strings.TrimSpace(string(pid))).Run())
		}
	})
	require.NoError(t, running.Process.Kill())
	require.Error(t, running.Wait())
	envelope, stderr, code := f.runJSON(wt, "check", "competitor")
	require.Equal(t, 7, code, "a live survivor must retain exclusion: %s %v", stderr, envelope["error"])
	_, stderr, code = f.run(f.repo, "remove", "feature-orphan", "--force")
	require.Equal(t, 7, code, stderr)
	require.DirExists(t, wt)
	require.Eventually(t, func() bool { return exec.Command("kill", "-0", strings.TrimSpace(string(pid))).Run() != nil }, 10*time.Second, 10*time.Millisecond, "supervisor death must terminate the complete command group")
	_, stderr, code = f.runJSON(wt, "check", "competitor", "--lock-wait", "5s")
	require.Zero(t, code, stderr)
}
