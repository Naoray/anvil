package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStatusAllReportsMalformedLeaseAsUnknown(t *testing.T) {
	f := newAgentFixture(t, "scaffold:\n  override: true\n", nil)
	_, stderr, code := f.runJSON(f.repo, "ensure", "broken-lease", "--skip-prepare")
	require.Equal(t, 0, code, stderr)
	statePath := filepath.Join(f.worktreePath("broken-lease"), ".anvil.local")
	state, err := os.ReadFile(statePath)
	require.NoError(t, err)
	state = append(state, []byte("\nlease: not-a-lease-object\n")...)
	require.NoError(t, os.WriteFile(statePath, state, 0o644))
	envelope, stderr, code := f.runJSON(f.repo, "status", "--all")
	require.Equal(t, 0, code, stderr)
	var found bool
	for _, entry := range field(t, envelope, "data", "worktrees").([]any) {
		if field(t, entry, "worktree", "name") == "broken-lease" {
			found = true
			require.Equal(t, "unknown", field(t, entry, "lease", "state"))
		}
	}
	require.True(t, found)
	warnings := field(t, envelope, "warnings").([]any)
	require.NotEmpty(t, warnings)
	require.Contains(t, warnings[0], "broken-lease")
	require.Contains(t, warnings[0], "malformed lease")
	after, err := os.ReadFile(statePath)
	require.NoError(t, err)
	require.Equal(t, state, after, "inspection never rewrites malformed state")
}
