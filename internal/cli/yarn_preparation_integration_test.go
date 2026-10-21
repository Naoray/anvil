package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureYarnLinkerPreparationIsIdempotent(t *testing.T) {
	for _, linker := range []string{"pnp", "node-modules"} {
		t.Run(linker, func(t *testing.T) {
			f := newAgentFixture(t, "components:\n  - name: web\n    path: web\n    uses: [node]\n", map[string]string{
				"web/package.json": "{}",
				"web/yarn.lock":    "lock",
				"web/.yarnrc.yml":  "nodeLinker: " + linker + "\n",
			})
			fakeToolchain(t, f, "node", "yarn")
			bin := filepath.SplitList(environValue(f.env, "PATH"))[0]
			script := "#!/bin/sh\necho run >> installer.count\ncase \"$(cat .yarnrc.yml)\" in\n *node-modules*) mkdir -p node_modules;;\n *) touch .pnp.cjs;;\nesac\n"
			require.NoError(t, os.WriteFile(filepath.Join(bin, "yarn"), []byte(script), 0o755))
			_, stderr, code := f.runJSON(f.repo, "ensure", "feature/yarn")
			require.Equal(t, 0, code, stderr)
			wt := f.worktreePath("feature-yarn")
			dir := filepath.Join(wt, "web")
			next, stderr, code := f.runJSON(f.repo, "ensure", "feature-yarn")
			require.Equal(t, 0, code, stderr)
			require.Equal(t, []string{"up_to_date"}, stepResults(t, next))
			require.Equal(t, 1, countRuns(t, filepath.Join(dir, "installer.count")))
			output, other := ".pnp.cjs", "node-modules"
			if linker == "node-modules" {
				output, other = "node_modules", "pnp"
			}
			require.NoError(t, os.RemoveAll(filepath.Join(dir, output)))
			next, stderr, code = f.runJSON(f.repo, "ensure", "feature-yarn")
			require.Equal(t, 0, code, stderr)
			require.Equal(t, []string{"ran"}, stepResults(t, next))
			require.Equal(t, 2, countRuns(t, filepath.Join(dir, "installer.count")))
			require.NoError(t, os.WriteFile(filepath.Join(dir, ".yarnrc.yml"), []byte("nodeLinker: "+other+"\n"), 0o644))
			next, stderr, code = f.runJSON(f.repo, "ensure", "feature-yarn")
			require.Equal(t, 0, code, stderr)
			require.Equal(t, []string{"ran"}, stepResults(t, next))
			require.Equal(t, 3, countRuns(t, filepath.Join(dir, "installer.count")))
		})
	}
}
