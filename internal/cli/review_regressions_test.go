package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReviewReensurePreservesMinimalPreparation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	f := newAgentFixture(t, lifecycleFixtureYAML, nil)
	for _, selector := range []string{"name", "path", "id", "cwd"} {
		t.Run(selector, func(t *testing.T) {
			name := "review-" + selector
			first, stderr, code := f.runJSON(f.repo, "ensure", name, "--review", "HEAD")
			require.Equal(t, 0, code, stderr)
			path := field(t, first, "data", "worktree", "path").(string)
			cwd, args := f.repo, []string{"ensure"}
			switch selector {
			case "name":
				args = append(args, name)
			case "path":
				args = append(args, path)
			case "id":
				args = append(args, field(t, first, "data", "worktree", "id").(string))
			case "cwd":
				cwd = path
			}
			again, stderr, code := f.runJSON(cwd, args...)
			require.Equal(t, 0, code, stderr)
			require.Equal(t, "review", field(t, again, "data", "metadata", "role"))
			require.Equal(t, true, field(t, again, "data", "prepare", "ready"))
			require.NoFileExists(t, filepath.Join(path, "scaffold.marker"), "persisted review role keeps scaffold disabled")
			require.FileExists(t, filepath.Join(path, "component.marker"))

			_, stderr, code = f.runJSON(cwd, append(args, "--full-prepare")...)
			require.Equal(t, 0, code, stderr)
			require.FileExists(t, filepath.Join(path, "scaffold.marker"))
			require.NoError(t, os.Remove(filepath.Join(path, "scaffold.marker")))
		})
	}
}
