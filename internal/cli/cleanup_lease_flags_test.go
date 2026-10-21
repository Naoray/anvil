package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCleanupCommandsAcceptExplicitLeaseOverride(t *testing.T) {
	for _, command := range []string{"prune", "unlink"} {
		t.Run(command, func(t *testing.T) {
			f := newAgentFixture(t, "scaffold:\n  override: true\n", nil)
			gitRun(t, f.repo, "remote", "add", "origin", f.repo)
			_, stderr, code := f.runJSON(f.repo, "ensure", "leased", "--lease-owner", "worker")
			require.Equal(t, 0, code, stderr)
			path := f.worktreePath("leased")
			args := []string{command, "--force"}
			if command == "unlink" {
				args = append(args, "--clean")
			}
			stdout, stderr, code := f.run(f.repo, args...)
			require.NotEqual(t, 0, code, stdout+stderr)
			require.Contains(t, stdout+stderr, "leased by")
			require.DirExists(t, path)

			stdout, stderr, code = f.run(f.repo, append(args, "--ignore-lease")...)
			require.Equal(t, 0, code, stdout+stderr)
			require.Contains(t, stdout+stderr, "--ignore-lease")
			require.NoDirExists(t, path)
		})
	}
}
