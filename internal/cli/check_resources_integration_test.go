package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func statusCheckEntry(t *testing.T, envelope map[string]any, name string) map[string]any {
	t.Helper()
	for _, raw := range field(t, envelope, "data", "checks").([]any) {
		if raw.(map[string]any)["name"] == name {
			return raw.(map[string]any)
		}
	}
	t.Fatalf("check %s not in status", name)
	return nil
}

func TestCheckSharedResourceIdentityThroughBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("check commands use sh")
	}
	shared := evalSymlinks(t.TempDir())
	models, weights, fixtures, unrelated := filepath.Join(shared, "models"), filepath.Join(shared, "weights"), filepath.Join(shared, "fixtures"), filepath.Join(shared, "unrelated")
	for path, content := range map[string]string{
		filepath.Join(models, "SHA256SUMS"):    "aaa  model.bin\n",
		filepath.Join(weights, "layer", "0"):   "0000",
		filepath.Join(fixtures, "version"):     "v1\n",
		filepath.Join(unrelated, "SHA256SUMS"): "x\n",
	} {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	yaml := fmt.Sprintf(`resources:
  shared:
    - {name: models, path: %q, identity: {file: SHA256SUMS}}
    - {name: weights, path: %q, identity: {directory: true}}
    - {name: fixtures, path: %q, identity: {command: "cat version"}}
    - {name: unrelated, path: %q, identity: {file: SHA256SUMS}}
checks:
  fast:
    commands: ["echo run >> fast.count"]
    resources: [weights, models, fixtures]
`, models, weights, fixtures, unrelated)
	f := newAgentFixture(t, yaml, nil)
	_, stderr, code := f.runJSON(f.repo, "ensure", "feature/r")
	require.Equal(t, 0, code, stderr)
	wt := f.worktreePath("feature-r")

	envelope, stderr, code := f.runJSON(wt, "check", "fast")
	require.Equal(t, 0, code, stderr)
	identity := field(t, envelope, "data", "results", 0, "identity").(map[string]any)
	require.NotEmpty(t, identity["resources_digest"])
	var names []string
	for _, raw := range identity["resources"].([]any) {
		entry := raw.(map[string]any)
		names = append(names, entry["name"].(string))
		require.Equal(t, "present", entry["state"])
		require.NotEmpty(t, entry["digest"])
	}
	require.Equal(t, []string{"fixtures", "models", "weights"}, names)

	assessment := func() map[string]any {
		envelope, stderr, code := f.runJSON(wt, "check", "--list")
		require.Equal(t, 0, code, stderr)
		return field(t, checkEntry(t, envelope, "fast"), "assessment").(map[string]any)
	}
	require.Equal(t, true, assessment()["passing_and_current"])

	// An undeclared resource never affects the check.
	require.NoError(t, os.WriteFile(filepath.Join(unrelated, "SHA256SUMS"), []byte("y\n"), 0o644))
	require.Equal(t, true, assessment()["passing_and_current"])

	require.NoError(t, os.WriteFile(filepath.Join(models, "SHA256SUMS"), []byte("bbb  model.bin\n"), 0o644))
	require.Equal(t, []any{`shared resource "models" changed`}, assessment()["stale_reasons"])
	require.NoError(t, os.WriteFile(filepath.Join(weights, "layer", "0"), []byte("1111"), 0o644))
	require.Equal(t, []any{`shared resource "models" changed`, `shared resource "weights" changed`}, assessment()["stale_reasons"])
	status, stderr, code := f.runJSON(f.repo, "status", "feature-r")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, []any{`shared resource "models" changed`, `shared resource "weights" changed`}, statusCheckEntry(t, status, "fast")["stale_reasons"])

	_, stderr, code = f.runJSON(wt, "check", "fast")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, true, assessment()["passing_and_current"])

	// An identity that cannot be computed never runs, reuses or reports current.
	require.NoError(t, os.Remove(filepath.Join(fixtures, "version")))
	before := countRuns(t, filepath.Join(wt, "fast.count"))
	envelope, _, code = f.runJSON(wt, "check", "fast")
	require.NotEqual(t, 0, code)
	require.Equal(t, "check_failed", field(t, envelope, "error", "code"))
	require.Contains(t, field(t, envelope, "error", "message"), `identity unavailable: shared resource "fixtures"`)
	require.Equal(t, "fixtures", field(t, envelope, "error", "details", "resource"))
	require.Equal(t, "identity_unavailable", field(t, envelope, "error", "details", "reason"))
	require.Equal(t, before, countRuns(t, filepath.Join(wt, "fast.count")))
	_, _, code = f.runJSON(wt, "check", "fast", "--reuse")
	require.NotEqual(t, 0, code)
	list := assessment()
	require.Equal(t, false, list["current"])
	require.Len(t, list["stale_reasons"], 1)
	require.Contains(t, field(t, list, "stale_reasons", 0), `identity unavailable: shared resource "fixtures"`)
	status, _, _ = f.runJSON(f.repo, "status", "feature-r")
	require.Contains(t, field(t, statusCheckEntry(t, status, "fast"), "stale_reasons", 0), "identity unavailable")
}

func TestCheckSharedResourceCacheIsBestEffortThroughBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("check commands use sh")
	}
	weights := evalSymlinks(t.TempDir())
	require.NoError(t, os.WriteFile(filepath.Join(weights, "0"), []byte("0"), 0o644))
	f := newAgentFixture(t, fmt.Sprintf(`resources:
  shared:
    - {name: weights, path: %q, identity: {directory: true}}
checks:
  fast:
    commands: ["true"]
    resources: [weights]
`, weights), nil)
	_, stderr, code := f.runJSON(f.repo, "ensure", "feature/w")
	require.Equal(t, 0, code, stderr)
	wt := f.worktreePath("feature-w")
	// A regular file where the cache directory belongs never blocks identity.
	cache := filepath.Join(f.runtimeRoot(), "cache", "resource-identity")
	require.NoError(t, os.MkdirAll(filepath.Dir(cache), 0o755))
	require.NoError(t, os.WriteFile(cache, []byte("not a directory"), 0o644))

	envelope, stderr, code := f.runJSON(wt, "check", "fast")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, "passed", field(t, envelope, "data", "results", 0, "status"))
	envelope, stderr, code = f.runJSON(wt, "check", "--list")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, true, field(t, checkEntry(t, envelope, "fast"), "assessment", "passing_and_current"))
}
