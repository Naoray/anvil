package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const consistencyFixtureYAML = "scaffold:\n  override: true\n"

// statusAllEntryFor finds the status --all entry whose worktree path is path.
func statusAllEntryFor(t *testing.T, envelope map[string]any, path string) map[string]any {
	t.Helper()
	for _, raw := range field(t, envelope, "data", "worktrees").([]any) {
		if field(t, raw, "worktree", "path") == path {
			return raw.(map[string]any)
		}
	}
	require.Failf(t, "worktree missing from status --all", "path %s", path)
	return nil
}

// reportedPath returns the worktree.path a status --all entry reports.
// Per-worktree warnings must name the worktree by this exact string so
// agents can correlate them with the entry on every platform.
func reportedPath(t *testing.T, entry map[string]any) string {
	t.Helper()
	path, ok := field(t, entry, "worktree", "path").(string)
	require.True(t, ok, "worktree.path must be a string")
	require.NotEmpty(t, path)
	return path
}

// leaseFrom returns the lease object reported by context, status and
// status --all for the same worktree.
func leasesFrom(t *testing.T, f *agentFixture, name string) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, command := range []string{"context", "status"} {
		envelope, stderr, code := f.runJSON(f.repo, command, name)
		require.Equal(t, 0, code, stderr)
		lease, ok := field(t, envelope, "data", "lease").(map[string]any)
		require.True(t, ok, "%s data.lease must be an object", command)
		out[command] = lease
	}
	all, stderr, code := f.runJSON(f.repo, "status", "--all")
	require.Equal(t, 0, code, stderr)
	entry := statusAllEntryFor(t, all, f.worktreePath(name))
	lease, ok := entry["lease"].(map[string]any)
	require.True(t, ok, "status --all lease must be an object, got %T", entry["lease"])
	out["status --all"] = lease
	return out
}

func TestAgentLeaseShapeIsConsistentAcrossCommands(t *testing.T) {
	f := newAgentFixture(t, consistencyFixtureYAML, nil)
	_, stderr, code := f.runJSON(f.repo, "ensure", "feature/lease")
	require.Equal(t, 0, code, stderr)

	for command, lease := range leasesFrom(t, f, "feature-lease") {
		require.Equal(t, "none", lease["state"], command)
	}

	acquired, stderr, code := f.runJSON(f.repo, "lease", "acquire", "feature-lease", "--owner", "codex", "--task-id", "T-1")
	require.Equal(t, 0, code, stderr)
	token := field(t, acquired, "data", "lease", "token").(string)
	require.NotEmpty(t, token)

	leases := leasesFrom(t, f, "feature-lease")
	for command, lease := range leases {
		require.Equal(t, "active", lease["state"], command)
		require.Equal(t, "codex", lease["owner"], command)
		require.Equal(t, "T-1", lease["task_id"], command)
		require.NotEmpty(t, lease["expires_at"], command)
		require.NotContains(t, lease, "token", "%s never repeats the lease token", command)
	}
	require.Equal(t, leases["status"], leases["status --all"], "status --all reports the same lease object as status")
	require.Equal(t, leases["context"], leases["status"])
}

func TestStatusAllReportsReadinessAndManagedIdentity(t *testing.T) {
	f := newAgentFixture(t, consistencyFixtureYAML, nil)
	_, stderr, code := f.runJSON(f.repo, "ensure", "feature/prepared")
	require.Equal(t, 0, code, stderr)
	plain := filepath.Join(f.base, "plain")
	gitRun(t, f.repo, "worktree", "add", "-b", "plain", plain)
	plain = evalSymlinks(plain)

	all, stderr, code := f.runJSON(f.repo, "status", "--all")
	require.Equal(t, 0, code, stderr)

	prepared := statusAllEntryFor(t, all, f.worktreePath("feature-prepared"))
	require.Equal(t, true, field(t, prepared, "worktree", "managed"))
	require.Equal(t, "ok", field(t, prepared, "worktree", "identity_status"))
	require.Equal(t, true, prepared["ready"])
	require.Equal(t, "complete", field(t, prepared, "prepare", "status"))
	require.Equal(t, true, field(t, prepared, "prepare", "ready"))
	require.NotEmpty(t, field(t, prepared, "prepare", "finished_at"))
	require.Equal(t, false, prepared["dirty"])

	for name, path := range map[string]string{"main checkout": f.repo, "plain git worktree": plain} {
		entry := statusAllEntryFor(t, all, path)
		require.Equal(t, false, field(t, entry, "worktree", "managed"), name)
		require.Equal(t, "missing", field(t, entry, "worktree", "identity_status"), name)
		require.Equal(t, false, entry["ready"], name)
		require.Equal(t, "unprepared", field(t, entry, "prepare", "status"), name)
		require.Equal(t, false, field(t, entry, "prepare", "ready"), name)
		require.Equal(t, "none", field(t, entry, "lease", "state"), name)
	}

	// Single status and context agree with the --all entry.
	for _, command := range []string{"status", "context"} {
		envelope, stderr, code := f.runJSON(f.repo, command, "feature-prepared")
		require.Equal(t, 0, code, stderr)
		require.Equal(t, true, field(t, envelope, "data", "worktree", "managed"), command)
		require.Equal(t, true, field(t, envelope, "data", "ready"), command)
		require.Equal(t, "complete", field(t, envelope, "data", "prepare", "status"), command)
		require.Equal(t, true, field(t, envelope, "data", "prepare", "ready"), command)

		envelope, stderr, code = f.runJSON(f.repo, command, plain)
		require.Equal(t, 0, code, stderr)
		require.Equal(t, false, field(t, envelope, "data", "worktree", "managed"), command)
		require.Equal(t, false, field(t, envelope, "data", "ready"), command)
		require.Equal(t, "unprepared", field(t, envelope, "data", "prepare", "status"), command)
	}

	// ensure reports managed and top-level readiness too.
	ensured, stderr, code := f.runJSON(f.repo, "ensure", "feature/prepared")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, true, field(t, ensured, "data", "worktree", "managed"))
	require.Equal(t, true, field(t, ensured, "data", "ready"))

	// Human output still renders the summarized fields.
	stdout, stderr, code := f.run(f.repo, "status", "--all")
	require.Equal(t, 0, code, stderr)
	require.Contains(t, stdout, "feature-prepared")
	require.Regexp(t, `feature-prepared\s+role=\S*\s+prepare=complete\s+ready=true\s+managed=true\s+lease=none`, stdout)
}

var versionLinePattern = regexp.MustCompile(`^anvil version \S+ \(commit: \S+, built: \S+\)\n$`)

func TestVersionFlagAndCommandAgree(t *testing.T) {
	f := newAgentFixture(t, consistencyFixtureYAML, nil)
	// A fresh global configuration (no setup_complete) outside CI would
	// trigger the first-run wizard for ordinary commands.
	configHome := t.TempDir()
	f.env = subprocessEnvFrom(append(append([]string{}, f.env...), "XDG_CONFIG_HOME="+configHome, "CI="))

	flagOut, stderr, code := f.run(f.repo, "--version")
	require.Equal(t, 0, code, stderr)
	require.Regexp(t, versionLinePattern, flagOut)

	cmdOut, stderr, code := f.run(f.repo, "version")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, flagOut, cmdOut)

	entries, err := os.ReadDir(configHome)
	require.NoError(t, err)
	require.Empty(t, entries, "version never runs first-run setup or writes configuration")

	envelope, stderr, code := f.runJSON(f.repo, "version")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, "version", envelope["command"])
	require.Equal(t, true, envelope["ok"])
	require.Equal(t, false, envelope["mutating"])
	require.NotEmpty(t, field(t, envelope, "data", "version"))
	require.NotEmpty(t, field(t, envelope, "data", "commit"))
	require.NotEmpty(t, field(t, envelope, "data", "build_date"))
	require.NotEmpty(t, field(t, envelope, "data", "go_version"))
	require.Equal(t, runtime.GOOS, field(t, envelope, "data", "os"))
	require.Equal(t, runtime.GOARCH, field(t, envelope, "data", "arch"))
	require.Contains(t, flagOut, "anvil version "+field(t, envelope, "data", "version").(string)+" ")

	shortOut, stderr, code := f.run(f.repo, "-v")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, flagOut, shortOut)

	_, _, code = f.run(f.repo, "version", "extra")
	require.Equal(t, 2, code, "version takes no arguments")
	rejected, _, code := f.runJSON(f.repo, "version", "extra")
	require.Equal(t, 2, code)
	require.Equal(t, "invalid_arguments", field(t, rejected, "error", "code"))
}

func warningsContain(t *testing.T, envelope map[string]any, parts ...string) bool {
	t.Helper()
	for _, raw := range field(t, envelope, "warnings").([]any) {
		matched := true
		for _, part := range parts {
			matched = matched && strings.Contains(raw.(string), part)
		}
		if matched {
			return true
		}
	}
	return false
}

func TestReadinessHonorsMissingSharedResourceEverywhere(t *testing.T) {
	shared := filepath.Join(t.TempDir(), "models")
	require.NoError(t, os.MkdirAll(shared, 0o755))
	yaml := fmt.Sprintf("scaffold:\n  override: true\nresources:\n  shared:\n    - name: models\n      path: %q\n      required: true\n", shared)
	f := newAgentFixture(t, yaml, nil)
	ensured, stderr, code := f.runJSON(f.repo, "ensure", "feature/shared")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, true, field(t, ensured, "data", "ready"))

	require.NoError(t, os.RemoveAll(shared))
	path := f.worktreePath("feature-shared")

	ensured, stderr, code = f.runJSON(f.repo, "ensure", "feature-shared", "--skip-prepare")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, false, field(t, ensured, "data", "ready"), "ensure --skip-prepare")
	require.Equal(t, true, field(t, ensured, "data", "prepare", "ready"))
	require.True(t, warningsContain(t, ensured, "required shared resources are missing"))

	for _, command := range []string{"status", "context"} {
		envelope, stderr, code := f.runJSON(f.repo, command, "feature-shared")
		require.Equal(t, 0, code, stderr)
		require.Equal(t, false, field(t, envelope, "data", "ready"), command)
		require.Equal(t, true, field(t, envelope, "data", "prepare", "ready"), command)
	}

	all, stderr, code := f.runJSON(f.repo, "status", "--all")
	require.Equal(t, 0, code, stderr)
	entry := statusAllEntryFor(t, all, path)
	require.Equal(t, false, entry["ready"])
	require.Equal(t, true, field(t, entry, "prepare", "ready"))
	require.True(t, warningsContain(t, all, reportedPath(t, entry), "required shared resources are missing"), "status --all names why a prepared worktree is not ready")
}

func TestStatusAllReportsUnavailableReadiness(t *testing.T) {
	f := newAgentFixture(t, consistencyFixtureYAML, nil)
	for _, name := range []string{"feature/good", "feature/broken"} {
		_, stderr, code := f.runJSON(f.repo, "ensure", name)
		require.Equal(t, 0, code, stderr)
	}
	broken := f.worktreePath("feature-broken")
	require.NoError(t, os.WriteFile(filepath.Join(broken, "anvil.yaml"), []byte("scaffold: [unclosed\n"), 0o644))

	all, stderr, code := f.runJSON(f.repo, "status", "--all")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, true, all["ok"])
	entry := statusAllEntryFor(t, all, broken)
	require.Equal(t, false, entry["ready"])
	require.Equal(t, true, field(t, entry, "prepare", "ready"))
	require.True(t, warningsContain(t, all, reportedPath(t, entry), "readiness unavailable"))
	good := statusAllEntryFor(t, all, f.worktreePath("feature-good"))
	require.Equal(t, true, good["ready"])
}

func TestMalformedPrepareRecordIsUnknownNotAbsent(t *testing.T) {
	f := newAgentFixture(t, consistencyFixtureYAML, nil)
	_, stderr, code := f.runJSON(f.repo, "ensure", "feature/bad-prepare")
	require.Equal(t, 0, code, stderr)
	path := f.worktreePath("feature-bad-prepare")
	statePath := filepath.Join(path, ".anvil.local")
	state, err := os.ReadFile(statePath)
	require.NoError(t, err)
	state = []byte(regexp.MustCompile(`(?m)^prepare:`).ReplaceAllString(string(state), "previous_prepare:") + "prepare: not-a-prepare-object\n")
	require.NoError(t, os.WriteFile(statePath, state, 0o644))

	context, stderr, code := f.runJSON(f.repo, "context", "feature-bad-prepare")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, "unknown", field(t, context, "data", "prepare", "status"))
	require.Equal(t, false, field(t, context, "data", "ready"))
	require.True(t, warningsContain(t, context, "preparation state unavailable"))

	all, stderr, code := f.runJSON(f.repo, "status", "--all")
	require.Equal(t, 0, code, stderr)
	entry := statusAllEntryFor(t, all, path)
	require.Equal(t, "unknown", field(t, entry, "prepare", "status"))
	require.Equal(t, false, entry["ready"])
	require.True(t, warningsContain(t, all, reportedPath(t, entry), "preparation state unavailable"))

	after, err := os.ReadFile(statePath)
	require.NoError(t, err)
	require.Equal(t, state, after, "inspection never rewrites malformed state")
}
