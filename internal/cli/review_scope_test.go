package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReviewScopeExcludesUnavailableComponentRequirements(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	for _, excluded := range []string{"requires: [anvil-test-nonexistent-desktop-tool]", "path: missing-desktop-directory"} {
		t.Run(excluded, func(t *testing.T) {
			cfg := `scaffold:
  override: true
components:
  - name: app
    prepare:
      - run: touch app.marker
        outputs: [app.marker]
  - name: desktop
    ` + excluded + `
    prepare:
      - run: touch desktop.marker
roles:
  review:
    components: [app]
`
			f := newAgentFixture(t, cfg, nil)
			review, stderr, code := f.runJSON(f.repo, "ensure", "scoped-review", "--review", "HEAD")
			require.Equal(t, 0, code, stderr)
			path := field(t, review, "data", "worktree", "path").(string)
			require.Equal(t, true, field(t, review, "data", "prepare", "ready"))
			require.FileExists(t, filepath.Join(path, "app.marker"))
			require.NoFileExists(t, filepath.Join(path, "desktop.marker"))
			for _, args := range [][]string{{"ensure", path}, {"status", path}, {"context", path}} {
				envelope, stderr, code := f.runJSON(f.repo, args...)
				require.Equal(t, 0, code, stderr)
				require.Empty(t, envelope["warnings"], "excluded requirements do not affect readiness or inspection")
				if args[0] == "status" {
					require.Equal(t, true, field(t, envelope, "data", "ready"))
				}
			}
			implementation, stderr, code := f.runJSON(f.repo, "ensure", "implementation")
			require.Equal(t, 6, code, stderr)
			require.Equal(t, "prepare_failed", field(t, implementation, "error", "code"))
			_, err := os.Stat(filepath.Join(f.worktreePath("implementation"), "app.marker"))
			require.True(t, os.IsNotExist(err), "implementation prerequisites fail before preparation")
		})
	}
}
