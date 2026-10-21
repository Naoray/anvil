package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureInvalidatesDeclaredPreparationEnvironment(t *testing.T) {
	yaml := "env:\n  MODE: one\n  API_TOKEN: super-secret-fixture-value\nscaffold:\n  override: true\n  steps:\n    - name: command.run\n      command: printf '%s' \"$MODE\" > root-mode.txt\ncomponents:\n  - name: tool\n    path: tool\n    prepare:\n      - run: printf '%s' \"$MODE\" > mode.txt\n"
	f := newAgentFixture(t, yaml, map[string]string{"tool/README.md": "tool"})
	fakeToolchain(t, f)
	first, stderr, code := f.runJSON(f.repo, "ensure", "feature/env-cache")
	require.Equal(t, 0, code, stderr)
	wt := f.worktreePath("feature-env-cache")
	_, stderr, code = f.runJSON(f.repo, "ensure", "feature-env-cache")
	require.Equal(t, 0, code, stderr)
	require.NoError(t, os.WriteFile(filepath.Join(wt, "anvil.yaml"), []byte(strings.Replace(yaml, "MODE: one", "MODE: two", 1)), 0o644))
	next, stderr, code := f.runJSON(f.repo, "ensure", "feature-env-cache")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, []string{"ran", "ran"}, stepResults(t, next))
	require.NotEqual(t, field(t, first, "data", "prepare", "plan_digest"), field(t, next, "data", "prepare", "plan_digest"))
	for _, path := range []string{"root-mode.txt", "tool/mode.txt"} {
		content, err := os.ReadFile(filepath.Join(wt, path))
		require.NoError(t, err)
		require.Equal(t, "two", string(content))
	}
	local, err := os.ReadFile(filepath.Join(wt, ".anvil.local"))
	require.NoError(t, err)
	require.NotContains(t, string(local), "super-secret-fixture-value")
	require.NoError(t, os.WriteFile(filepath.Join(wt, "anvil.yaml"), []byte(strings.Replace(yaml, "MODE: one", "MODE: \"${INPUT_MODE}\"", 1)), 0o644))
	f.env = subprocessEnvFrom(append(f.env, "INPUT_MODE=three"))
	_, stderr, code = f.runJSON(f.repo, "ensure", "feature-env-cache")
	require.Equal(t, 0, code, stderr)
	f.env = subprocessEnvFrom(append(f.env, "INPUT_MODE=four"))
	next, stderr, code = f.runJSON(f.repo, "ensure", "feature-env-cache")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, []string{"ran", "ran"}, stepResults(t, next))
	content, err := os.ReadFile(filepath.Join(wt, "tool", "mode.txt"))
	require.NoError(t, err)
	require.Equal(t, "four", string(content))
}

func TestEnsureInvalidatesInheritedComponentEnvironment(t *testing.T) {
	for _, placement := range []string{
		"    env:\n      MODE: \"${ANVIL_EXTERNAL_MODE}\"\n    prepare:\n      - run: printf '%s' \"$MODE\" > mode.txt\n",
		"    prepare:\n      - run: printf '%s' \"$MODE\" > mode.txt\n        env:\n          MODE: \"${ANVIL_EXTERNAL_MODE}\"\n",
	} {
		t.Run(placement, func(t *testing.T) {
			f := newAgentFixture(t, "components:\n  - name: tool\n    path: tool\n"+placement, map[string]string{"tool/README.md": "tool"})
			fakeToolchain(t, f)
			f.env = subprocessEnvFrom(append(f.env, "ANVIL_EXTERNAL_MODE=one"))
			_, stderr, code := f.runJSON(f.repo, "ensure", "feature/inherited")
			require.Equal(t, 0, code, stderr)
			f.env = subprocessEnvFrom(append(f.env, "ANVIL_EXTERNAL_MODE=two"))
			_, stderr, code = f.run(f.repo, "run", "feature-inherited", "--component", "tool", "--require-ready", "--", "true")
			require.Equal(t, 6, code, stderr)
			next, stderr, code := f.runJSON(f.repo, "ensure", "feature-inherited")
			require.Equal(t, 0, code, stderr)
			require.Equal(t, []string{"ran"}, stepResults(t, next))
			content, err := os.ReadFile(filepath.Join(f.worktreePath("feature-inherited"), "tool", "mode.txt"))
			require.NoError(t, err)
			require.Equal(t, "two", string(content))
		})
	}
}
