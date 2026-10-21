package steps

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/scaffold/types"
)

func TestStepEnvExpansionUsesImmutableBase(t *testing.T) {
	t.Setenv("ANVIL_EXPANSION_BASE", "parent")
	for _, base := range []map[string]string{nil, {"ANVIL_EXPANSION_BASE": "worktree"}} {
		ctx := &types.ScaffoldContext{WorktreePath: t.TempDir(), Env: base}
		expected := "parent"
		if base != nil {
			expected = "worktree"
		}
		for i := 0; i < 100; i++ {
			target := execTarget{env: map[string]string{
				"ANVIL_EXPANSION_BASE": "step",
				"VALUE":                "${ANVIL_EXPANSION_BASE}",
			}}
			_, env, err := target.resolve(ctx)
			require.NoError(t, err)
			require.Equal(t, "step", env["ANVIL_EXPANSION_BASE"])
			require.Equal(t, expected, env["VALUE"])
		}
		if base != nil {
			require.Equal(t, "worktree", base["ANVIL_EXPANSION_BASE"], "step overrides must not modify the base")
		}
	}
}

func TestCommandEnvironmentUsesWindowsVariableCasing(t *testing.T) {
	t.Setenv("ANVIL_MiXeD_Inherited", "parent")
	env := commandEnvironment(
		map[string]string{"Anvil_Mixed_Project": "worktree"},
		map[string]string{
			"ANVIL_MIXED_PROJECT": "step",
			"PROJECT_VALUE":       "$ANVIL_MIXED_PROJECT",
			"INHERITED_VALUE":     "$ANVIL_MIXED_INHERITED",
		}, true)
	require.Equal(t, "worktree", env["PROJECT_VALUE"])
	require.Equal(t, "parent", env["INHERITED_VALUE"])
	require.Equal(t, "step", env["ANVIL_MIXED_PROJECT"])
}

func TestCommandEnvironmentPreservesSpellingOfWindowsOverrides(t *testing.T) {
	env := commandEnvironment(
		map[string]string{"Mode": "base"},
		map[string]string{"mode": "step", "seen": "${MODE}"},
		true)
	require.Equal(t, "step", env["mode"])
	require.Equal(t, "base", env["seen"])
	require.NotContains(t, env, "Mode")
	require.NotContains(t, env, "MODE")
}
