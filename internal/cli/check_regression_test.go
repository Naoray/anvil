package cli

import (
	"bytes"
	"fmt"
	"github.com/naoray/anvil/internal/lockfile"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

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
