package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/lockfile"
)

const checksFixtureYAML = `checks:
  fast:
    description: unit tests
    commands:
      - echo run >> fast.count
      - run: test "$GREETING" = hello
        env: {GREETING: hello}
    toolchain: ["git --version"]
  failing:
    commands: ["exit 5"]
  slow:
    expensive: true
    commands: ["touch slow.started; sleep 30"]
  benchmark:
    description: expensive shared-GPU benchmark
    expensive: true
    reusable: true
    locks: [gpu]
    commands:
      - mkdir -p "$ANVIL_ARTIFACTS_DIR/bench" && echo "$ANVIL_GIT_STATE" > "$ANVIL_ARTIFACTS_DIR/bench/score.json" && echo run >> bench.count
    artifacts: ["${ANVIL_ARTIFACTS_DIR}/bench/*.json"]
`

func (f *agentFixture) runtimeRoot() string {
	for _, entry := range f.env {
		if value, ok := strings.CutPrefix(entry, "ANVIL_RUNTIME_ROOT="); ok {
			return value
		}
	}
	f.t.Fatal("fixture has no runtime root")
	return ""
}

func checkEntry(t *testing.T, envelope map[string]any, name string) map[string]any {
	t.Helper()
	for _, raw := range field(t, envelope, "data", "checks").([]any) {
		if raw.(map[string]any)["name"] == name {
			return raw.(map[string]any)
		}
	}
	t.Fatalf("check %s not listed", name)
	return nil
}

func TestCheckArgumentErrorsHaveJSONEnvelope(t *testing.T) {
	f := newAgentFixture(t, checksFixtureYAML, nil)
	for _, args := range [][]string{
		{"check", "--unknown-flag"},
		{"check", "--lock-wait=invalid"},
		{"check", "--verbose", "--quiet"},
	} {
		envelope, stderr, code := f.runJSON(f.repo, args...)
		require.Equal(t, 2, code, stderr)
		require.Equal(t, false, envelope["ok"])
		require.Equal(t, "check", envelope["command"])
		require.Equal(t, "invalid_arguments", field(t, envelope, "error", "code"))
	}
}

func TestCheckProvenanceStalenessFailuresAndLocks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("check commands use sh")
	}
	f := newAgentFixture(t, checksFixtureYAML, nil)
	_, stderr, code := f.runJSON(f.repo, "ensure", "feature/c")
	require.Equal(t, 0, code, stderr)
	wt := f.worktreePath("feature-c")

	envelope, _, code := f.runJSON(wt, "check", "--list")
	require.Equal(t, 0, code)
	require.Equal(t, false, envelope["mutating"])
	fast := checkEntry(t, envelope, "fast")
	require.Nil(t, fast["latest"])
	require.Equal(t, []any{"never run"}, field(t, fast, "assessment", "stale_reasons"))
	require.Equal(t, true, checkEntry(t, envelope, "benchmark")["expensive"])

	envelope, stderr, code = f.runJSON(wt, "check", "fast")
	require.Equal(t, 0, code, stderr)
	result := field(t, envelope, "data", "results", 0).(map[string]any)
	require.Equal(t, "passed", result["status"])
	require.Equal(t, false, field(t, result, "identity", "git", "dirty"))
	head := gitRun(t, wt, "rev-parse", "HEAD")
	require.Equal(t, head, field(t, result, "identity", "git", "head"))
	require.Contains(t, field(t, result, "identity", "toolchain", 0, "output"), "git version")
	require.Len(t, field(t, result, "commands"), 2)
	require.NotContains(t, stdoutOf(t, f, wt, "check", "fast", "--json"), "run\n", "child output never enters the JSON document")

	envelope, _, code = f.runJSON(wt, "check", "--list")
	require.Equal(t, 0, code)
	require.Equal(t, true, field(t, checkEntry(t, envelope, "fast"), "assessment", "passing_and_current"))

	// Dirty tree: the clean result is stale, never "current".
	require.NoError(t, os.WriteFile(filepath.Join(wt, "README.md"), []byte("changed\n"), 0o644))
	envelope, _, _ = f.runJSON(wt, "check", "--list")
	assessment := field(t, checkEntry(t, envelope, "fast"), "assessment").(map[string]any)
	require.Equal(t, false, assessment["passing_and_current"])
	require.Equal(t, []any{"uncommitted changes differ"}, assessment["stale_reasons"])
	envelope, _, code = f.runJSON(wt, "check", "fast")
	require.Equal(t, 0, code)
	require.Equal(t, true, field(t, envelope, "data", "results", 0, "identity", "git", "dirty"))
	// Committing the same content produces a new commit: still stale.
	gitRun(t, wt, "commit", "-am", "change readme")
	envelope, _, _ = f.runJSON(wt, "check", "--list")
	require.Contains(t, field(t, checkEntry(t, envelope, "fast"), "assessment", "stale_reasons"), "commit changed")

	// Failure passes the child's status through and is never current-passing.
	envelope, _, code = f.runJSON(wt, "check", "failing")
	require.Equal(t, 5, code)
	require.Equal(t, "check_failed", field(t, envelope, "error", "code"))
	require.Equal(t, "failed", field(t, envelope, "data", "results", 0, "status"))

	// No name: all non-expensive checks in order; first failure decides exit.
	envelope, _, code = f.runJSON(wt, "check")
	require.Equal(t, 5, code)
	results := field(t, envelope, "data", "results").([]any)
	require.Len(t, results, 2)
	require.Equal(t, "fast", field(t, results[0], "check"))

	// Expensive checks need explicit opt-in.
	envelope, _, code = f.runJSON(wt, "check", "benchmark")
	require.Equal(t, 2, code)
	require.Equal(t, "expensive_check_requires_opt_in", field(t, envelope, "error", "code"))
	require.NoFileExists(t, filepath.Join(wt, "bench.count"))

	envelope, stderr, code = f.runJSON(wt, "check", "benchmark", "--allow-expensive")
	require.Equal(t, 0, code, stderr)
	artifacts := field(t, envelope, "data", "results", 0, "artifacts").([]any)
	require.Len(t, artifacts, 1)
	require.Len(t, field(t, artifacts[0], "sha256"), 64)

	// Conservative reuse: same identity and intact artifacts → no rerun.
	envelope, _, code = f.runJSON(wt, "check", "benchmark", "--allow-expensive", "--reuse")
	require.Equal(t, 0, code)
	require.Equal(t, true, field(t, envelope, "data", "results", 0, "reused"))
	require.Equal(t, 1, countRuns(t, filepath.Join(wt, "bench.count")))
	artifact := field(t, artifacts[0], "path").(string)
	require.NoError(t, os.WriteFile(artifact, []byte("tampered"), 0o644))
	envelope, _, code = f.runJSON(wt, "check", "benchmark", "--allow-expensive", "--reuse")
	require.Equal(t, 0, code)
	require.Equal(t, false, field(t, envelope, "data", "results", 0, "reused"))
	require.Contains(t, strings.Join(stringsOf(envelope["warnings"]), "\n"), "changed")
	require.Equal(t, 2, countRuns(t, filepath.Join(wt, "bench.count")))

	// Exclusive lock held by another live process: fail fast, run nothing.
	lock, err := lockfile.Acquire(filepath.Join(f.runtimeRoot(), "locks", "gpu.lock"))
	require.NoError(t, err)
	envelope, _, code = f.runJSON(wt, "check", "benchmark", "--allow-expensive")
	require.Equal(t, 7, code)
	require.Equal(t, "resource_busy", field(t, envelope, "error", "code"))
	require.Equal(t, 2, countRuns(t, filepath.Join(wt, "bench.count")))
	require.NoError(t, lock.Release())

	_, _, code = f.runJSON(wt, "check", "nope")
	require.Equal(t, 3, code)
}

func TestRemoveRefusesWhileCheckRunning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("check commands use sh")
	}
	f := newAgentFixture(t, checksFixtureYAML, nil)
	_, stderr, code := f.runJSON(f.repo, "ensure", "feature/busy")
	require.Equal(t, 0, code, stderr)
	wt := f.worktreePath("feature-busy")
	running := exec.Command(f.bin, "check", "slow", "--allow-expensive", "--quiet")
	running.Dir = wt
	running.Env = f.env
	require.NoError(t, running.Start())
	require.Eventually(t, func() bool { _, err := os.Stat(filepath.Join(wt, "slow.started")); return err == nil }, 10*time.Second, 20*time.Millisecond)

	stdout, stderr, code := f.run(f.repo, "remove", "feature-busy", "--force")
	require.Equal(t, 7, code, stdout+stderr)
	require.Contains(t, stderr, "checks are running")
	require.DirExists(t, wt)

	require.NoError(t, running.Process.Signal(os.Interrupt))
	require.Error(t, running.Wait(), "interrupted check exits nonzero")
	envelope, _, _ := f.runJSON(wt, "check", "--list")
	require.Equal(t, "interrupted", field(t, checkEntry(t, envelope, "slow"), "latest", "status"))
	_, _, code = f.run(f.repo, "remove", "feature-busy", "--force")
	require.Equal(t, 0, code)
}

func stdoutOf(t *testing.T, f *agentFixture, dir string, args ...string) string {
	t.Helper()
	stdout, _, _ := f.run(dir, args...)
	return stdout
}

func stringsOf(value any) []string {
	var out []string
	for _, item := range value.([]any) {
		out = append(out, item.(string))
	}
	return out
}
