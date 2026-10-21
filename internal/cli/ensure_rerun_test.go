package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const rerunFixtureYAML = `scaffold:
  override: true
  steps:
    - name: command.run
      command: "if test -f fail-rerun; then echo broken > product; exit 1; fi; if test -f interrupt-rerun; then echo broken > product; touch entered; exec sleep 60; fi; echo good > product"
      inputs: [deps.lock]
      outputs: [product]
`

func TestEnsureRepairsFailedRerun(t *testing.T) {
	for _, trigger := range []string{"refresh", "inputs", "outputs"} {
		t.Run(trigger, func(t *testing.T) {
			f := newAgentFixture(t, rerunFixtureYAML, map[string]string{"deps.lock": "v1"})
			_, stderr, code := f.runJSON(f.repo, "ensure")
			require.Equal(t, 0, code, stderr)
			args := []string{"ensure"}
			switch trigger {
			case "refresh":
				args = append(args, "--refresh")
			case "inputs":
				require.NoError(t, os.WriteFile(filepath.Join(f.repo, "deps.lock"), []byte("v2"), 0o644))
			case "outputs":
				require.NoError(t, os.Remove(filepath.Join(f.repo, "product")))
			}
			gate := filepath.Join(f.repo, "fail-rerun")
			require.NoError(t, os.WriteFile(gate, nil, 0o644))
			envelope, stderr, code := f.runJSON(f.repo, args...)
			require.Equal(t, 6, code, stderr)
			require.Equal(t, false, field(t, envelope, "data", "prepare", "ready"))
			require.NoError(t, os.Remove(gate))
			envelope, stderr, code = f.runJSON(f.repo, "ensure")
			require.Equal(t, 0, code, stderr)
			require.Equal(t, []string{"ran"}, stepResults(t, envelope))
			product, err := os.ReadFile(filepath.Join(f.repo, "product"))
			require.NoError(t, err)
			require.Equal(t, "good\n", string(product))
			require.Equal(t, true, field(t, envelope, "data", "prepare", "ready"))
		})
	}
}
