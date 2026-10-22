package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// addGitWorktree creates a worktree with plain Git (no Anvil state) under the
// fixture's worktree base, on a new branch from main.
func (f *agentFixture) addGitWorktree(branch, folder string) string {
	f.t.Helper()
	path := f.worktreePath(folder)
	require.NoError(f.t, os.MkdirAll(filepath.Dir(path), 0o755))
	gitRun(f.t, f.repo, "worktree", "add", "-b", branch, path, "main")
	return path
}

func adoptEntries(t *testing.T, envelope map[string]any) map[string]map[string]any {
	t.Helper()
	entries := map[string]map[string]any{}
	for _, raw := range field(t, envelope, "data", "worktrees").([]any) {
		entry := raw.(map[string]any)
		entries[field(t, entry, "worktree", "name").(string)] = entry
	}
	return entries
}

func TestAdoptSingleWorktreeIsIdempotent(t *testing.T) {
	f := newAgentFixture(t, "scaffold:\n  override: true\n", nil)
	path := f.addGitWorktree("feature/adopt", "feature-adopt")

	envelope, stderr, code := f.runJSON(f.repo, "adopt", "feature-adopt")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, true, envelope["mutating"])
	entry := adoptEntries(t, envelope)["feature-adopt"]
	require.Equal(t, "adopted", entry["action"])
	id := field(t, entry, "worktree", "id").(string)
	require.Regexp(t, `^wt_[0-9a-f]{16}$`, id)
	require.Equal(t, "ok", field(t, entry, "worktree", "identity_status"))
	require.Equal(t, "none", field(t, entry, "lease", "state"))
	require.FileExists(t, filepath.Join(path, ".anvil.local"))
	// Adoption never scaffolds: nothing but identity is recorded.
	require.NoFileExists(t, filepath.Join(path, ".env"))
	state, err := os.ReadFile(filepath.Join(path, ".anvil.local"))
	require.NoError(t, err)
	require.NotContains(t, string(state), "prepare")

	envelope, stderr, code = f.runJSON(f.repo, "adopt", "feature-adopt")
	require.Equal(t, 0, code, stderr)
	entry = adoptEntries(t, envelope)["feature-adopt"]
	require.Equal(t, "unchanged", entry["action"])
	require.Equal(t, id, field(t, entry, "worktree", "id"))
}

func TestAdoptDryRunWritesNothing(t *testing.T) {
	f := newAgentFixture(t, "scaffold:\n  override: true\n", nil)
	path := f.addGitWorktree("feature/preview", "feature-preview")

	envelope, stderr, code := f.runJSON(f.repo, "adopt", "feature-preview", "--dry-run", "--lease-owner", "codex")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, true, field(t, envelope, "data", "dry_run"))
	entry := adoptEntries(t, envelope)["feature-preview"]
	require.Equal(t, "adopted", entry["action"])
	require.NoFileExists(t, filepath.Join(path, ".anvil.local"), "--dry-run must not write .anvil.local")
	require.Nil(t, field(t, entry, "worktree", "id"))
}

func TestAdoptAllSkipsMainAndMissingWorktrees(t *testing.T) {
	f := newAgentFixture(t, "scaffold:\n  override: true\n", nil)
	f.addGitWorktree("feature/one", "feature-one")
	f.addGitWorktree("feature/two", "feature-two")
	gone := f.addGitWorktree("feature/gone", "feature-gone")
	require.NoError(t, os.RemoveAll(gone))

	envelope, stderr, code := f.runJSON(f.repo, "adopt", "--all")
	require.Equal(t, 0, code, stderr)
	entries := adoptEntries(t, envelope)
	require.Equal(t, "adopted", entries["feature-one"]["action"])
	require.Equal(t, "adopted", entries["feature-two"]["action"])
	require.Equal(t, "skipped", entries["feature-gone"]["action"])
	require.Contains(t, entries["feature-gone"]["reason"], "missing")
	main := entries[filepath.Base(f.repo)]
	require.Equal(t, "skipped", main["action"])
	require.Contains(t, main["reason"], "main checkout")
	require.Equal(t, "unknown", field(t, entries["feature-gone"], "worktree", "identity_status"))
	require.Equal(t, "missing", field(t, main, "worktree", "identity_status"), "a skipped existing worktree is summarized")
	require.NoFileExists(t, filepath.Join(f.repo, ".anvil.local"))

	envelope, stderr, code = f.runJSON(f.repo, "adopt", "--all")
	require.Equal(t, 0, code, stderr)
	entries = adoptEntries(t, envelope)
	require.Equal(t, "unchanged", entries["feature-one"]["action"])
	require.Equal(t, "unchanged", entries["feature-two"]["action"])

	// The main checkout is adopted only when named explicitly.
	envelope, stderr, code = f.runJSON(f.repo, "adopt", f.repo)
	require.Equal(t, 0, code, stderr)
	require.Equal(t, "adopted", adoptEntries(t, envelope)[filepath.Base(f.repo)]["action"])

	_, _, code = f.runJSON(f.repo, "adopt", "--all", "feature-one")
	require.Equal(t, 2, code, "--all and WORKTREE are exclusive")
}

func TestAdoptMovedKeepsIDAndCopiedIsReassigned(t *testing.T) {
	f := newAgentFixture(t, "scaffold:\n  override: true\n", nil)
	f.addGitWorktree("feature/move", "feature-move")
	envelope, stderr, code := f.runJSON(f.repo, "adopt", "feature-move", "--lease-owner", "codex")
	require.Equal(t, 0, code, stderr)
	id := field(t, adoptEntries(t, envelope)["feature-move"], "worktree", "id")

	moved := f.worktreePath("feature-moved")
	gitRun(t, f.repo, "worktree", "move", f.worktreePath("feature-move"), moved)
	envelope, stderr, code = f.runJSON(f.repo, "adopt", "feature-moved")
	require.Equal(t, 0, code, stderr)
	entry := adoptEntries(t, envelope)["feature-moved"]
	require.Equal(t, "updated", entry["action"])
	require.Equal(t, id, field(t, entry, "worktree", "id"))
	require.Equal(t, "active", field(t, entry, "lease", "state"), "a moved worktree keeps its lease")

	copyPath := f.addGitWorktree("feature/copy", "feature-copy")
	state, err := os.ReadFile(filepath.Join(moved, ".anvil.local"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(copyPath, ".anvil.local"), state, 0o644))

	envelope, stderr, code = f.runJSON(f.repo, "adopt", "feature-copy")
	require.Equal(t, 0, code, stderr)
	entry = adoptEntries(t, envelope)["feature-copy"]
	require.Equal(t, "reassigned", entry["action"])
	require.NotEqual(t, id, field(t, entry, "worktree", "id"))
	require.Equal(t, "none", field(t, entry, "lease", "state"), "the copy drops the original's lease")

	envelope, stderr, code = f.runJSON(f.repo, "lease", "show", "feature-moved")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, "active", field(t, envelope, "data", "lease", "state"), "the original keeps its lease")
}

func TestAdoptLeaseIsPreservedAndNeverReplaced(t *testing.T) {
	f := newAgentFixture(t, "scaffold:\n  override: true\n", nil)
	path := f.addGitWorktree("feature/leased", "feature-leased")

	envelope, stderr, code := f.runJSON(f.repo, "adopt", "feature-leased", "--lease-owner", "codex", "--lease-ttl", "1h", "--session-id", "S1")
	require.Equal(t, 0, code, stderr)
	entry := adoptEntries(t, envelope)["feature-leased"]
	require.Equal(t, "active", field(t, entry, "lease", "state"))
	require.Equal(t, "S1", field(t, entry, "lease", "session_id"))
	token := field(t, entry, "lease", "token").(string)
	require.NotEmpty(t, token)

	// Plain re-adoption reports the lease and leaves it untouched.
	envelope, stderr, code = f.runJSON(f.repo, "adopt", "feature-leased")
	require.Equal(t, 0, code, stderr)
	entry = adoptEntries(t, envelope)["feature-leased"]
	require.Equal(t, "codex", field(t, entry, "lease", "owner"))
	require.Nil(t, field(t, entry, "lease", "token"))

	before, err := os.ReadFile(filepath.Join(path, ".anvil.local"))
	require.NoError(t, err)
	// Another owner never replaces an active lease, also when previewing.
	for _, args := range [][]string{
		{"adopt", "feature-leased", "--lease-owner", "claude"},
		{"adopt", "feature-leased", "--lease-owner", "claude", "--takeover-stale"},
		{"adopt", "feature-leased", "--lease-owner", "claude", "--dry-run"},
	} {
		envelope, _, code = f.runJSON(f.repo, args...)
		require.Equal(t, 7, code, args)
		require.Equal(t, "lease_held", field(t, envelope, "error", "code"), args)
		require.Equal(t, "codex", field(t, envelope, "error", "details", "lease", "owner"), args)
	}
	after, err := os.ReadFile(filepath.Join(path, ".anvil.local"))
	require.NoError(t, err)
	require.Equal(t, string(before), string(after))

	// The holder renews with its token.
	owner := withEnv(f, "ANVIL_LEASE_TOKEN="+token)
	envelope, stderr, code = owner.runJSON(f.repo, "adopt", "feature-leased", "--lease-owner", "codex")
	require.Equal(t, 0, code, stderr)
	entry = adoptEntries(t, envelope)["feature-leased"]
	require.Equal(t, "active", field(t, entry, "lease", "state"))
	require.Nil(t, field(t, entry, "lease", "token"), "renewal never reissues the token")
}

func TestAdoptDryRunWithTokenButNoLease(t *testing.T) {
	f := newAgentFixture(t, "scaffold:\n  override: true\n", nil)
	path := f.addGitWorktree("feature/token", "feature-token")
	withToken := withEnv(f, "ANVIL_LEASE_TOKEN=lt_previous_token")
	envelope, stderr, code := withToken.runJSON(f.repo, "adopt", "feature-token", "--lease-owner", "codex", "--dry-run")
	require.Equal(t, 0, code, stderr)
	entry := adoptEntries(t, envelope)["feature-token"]
	require.Equal(t, "adopted", entry["action"])
	require.Equal(t, "none", field(t, entry, "lease", "state"))
	require.NoFileExists(t, filepath.Join(path, ".anvil.local"))
}

func TestAdoptAllPrefixesLeaseWarningsWithPath(t *testing.T) {
	f := newAgentFixture(t, "scaffold:\n  override: true\n", nil)
	f.addGitWorktree("feature/leased", "feature-leased")
	f.addGitWorktree("feature/free", "feature-free")
	_, stderr, code := f.runJSON(f.repo, "adopt", "feature-leased", "--lease-owner", "codex")
	require.Equal(t, 0, code, stderr)
	envelope, stderr, code := f.runJSON(f.repo, "adopt", "--all")
	require.Equal(t, 0, code, stderr)
	warnings := field(t, envelope, "warnings").([]any)
	require.Len(t, warnings, 1)
	// Git may spell paths differently from the OS (C:/ vs C:\); the warning
	// must name the leased worktree exactly as its JSON entry reports it.
	leased := field(t, adoptEntries(t, envelope)["feature-leased"], "worktree", "path").(string)
	require.True(t, strings.HasPrefix(warnings[0].(string), leased+": "), warnings[0])
}
