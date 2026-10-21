package capabilities

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestYarnPreparationTracksLinkerOutputs(t *testing.T) {
	for _, tc := range []struct {
		config  string
		outputs []string
	}{
		{"{}", []string{".pnp.cjs"}},
		{"nodeLinker: pnp", []string{".pnp.cjs"}},
		{"nodeLinker: node-modules", []string{"node_modules"}},
		{"nodeLinker: pnpm", []string{"node_modules"}},
		{"nodeLinker: pnp\npnpEnableEsmLoader: true\npnpEnableInlining: false", []string{".pnp.cjs", ".pnp.loader.mjs", ".pnp.data.json"}},
	} {
		t.Run(tc.config, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, "package.json", "yarn.lock")
			require.NoError(t, os.WriteFile(filepath.Join(dir, ".yarnrc.yml"), []byte(tc.config), 0o644))
			contribution := Contribute("node", dir, "linux")
			require.Len(t, contribution.Prepare, 1)
			require.Equal(t, tc.outputs, contribution.Prepare[0].Outputs)
			require.Contains(t, contribution.Prepare[0].Inputs, ".yarnrc.yml")
		})
	}
}
