package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureEarlyFailureKeepsResolvedWorktree(t *testing.T) {
	for _, failure := range []string{"flags", "config", "identity"} {
		t.Run(failure, func(t *testing.T) {
			f := newAgentFixture(t, ensureFixtureYAML, nil)
			args := []string{"ensure", "feature/partial"}
			expectedCode := 2
			if failure == "flags" {
				args = append(args, "--db-action=invalid")
			} else {
				_, stderr, code := f.runJSON(f.repo, "ensure", "feature/partial", "--skip-prepare")
				require.Equal(t, 0, code, stderr)
				path := filepath.Join(f.worktreePath("feature-partial"), "anvil.yaml")
				expectedCode = 5
				if failure == "identity" {
					path = filepath.Join(f.worktreePath("feature-partial"), ".anvil.local")
					args = append(args, "--skip-prepare")
					expectedCode = 1
				}
				require.NoError(t, os.WriteFile(path, []byte("invalid: ["), 0o644))
			}
			envelope, stderr, code := f.runJSON(f.repo, args...)
			require.Equal(t, expectedCode, code, stderr)
			require.Equal(t, false, envelope["ok"])
			require.Equal(t, f.worktreePath("feature-partial"), field(t, envelope, "data", "worktree", "path"))
			require.Equal(t, "feature/partial", field(t, envelope, "data", "worktree", "branch"))
			require.Equal(t, failure == "flags", field(t, envelope, "data", "created"))
			if failure == "identity" {
				require.NotEmpty(t, envelope["warnings"], "identity read diagnostics must be retained")
			}
		})
	}
}
