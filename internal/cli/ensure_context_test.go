package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureEnvReadRebuildsContextEveryRun(t *testing.T) {
	f := newAgentFixture(t, `scaffold:
  override: true
  steps:
    - name: env.read
      key: FIXTURE_VALUE
    - name: bash.run
      command: echo '{{ .FIXTURE_VALUE }}' > always-result
      always: true
    - name: bash.run
      command: echo '{{ .FIXTURE_VALUE }}' > input-result
      inputs: [deps.lock]
    - name: command.run
      command: echo matched >> condition-result
      always: true
      condition: {context_var: {name: FIXTURE_VALUE, value: fixture}}
`, map[string]string{".env": "FIXTURE_VALUE=fixture\n", "deps.lock": "v1"})
	for _, version := range []string{"v1", "v2"} {
		require.NoError(t, os.WriteFile(filepath.Join(f.repo, "deps.lock"), []byte(version), 0o644))
		envelope, stderr, code := f.runJSON(f.repo, "ensure")
		require.Equal(t, 0, code, stderr)
		require.Equal(t, []string{"ran", "ran", "ran", "ran"}, stepResults(t, envelope))
		for _, output := range []string{"always-result", "input-result"} {
			content, err := os.ReadFile(filepath.Join(f.repo, output))
			require.NoError(t, err)
			require.Equal(t, "fixture\n", string(content))
		}
	}
	content, err := os.ReadFile(filepath.Join(f.repo, "condition-result"))
	require.NoError(t, err)
	require.Equal(t, "matched\nmatched\n", string(content))
}
