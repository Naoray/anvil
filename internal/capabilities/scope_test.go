package capabilities

import (
	"testing"

	"github.com/naoray/anvil/internal/config"
	"github.com/stretchr/testify/require"
)

func TestPlanScopesRequirementsBeforeEvaluation(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{Components: []config.Component{
		{Name: "app", Prepare: []config.PrepareCommand{{Run: "true"}}},
		{Name: "desktop", Requires: []string{"unavailable-tool"}},
		{Name: "helper", Path: "missing-directory"},
	}}
	opts := Options{GOOS: "linux", LookPath: lookPath(), Scope: []string{"app"}, Selected: []string{"desktop"}}
	plans, err := Plan(root, cfg, opts)
	require.NoError(t, err)
	require.Len(t, plans, 3)
	require.Equal(t, StatusActive, plans[0].Status)
	for _, excluded := range plans[1:] {
		require.Equal(t, StatusSkipped, excluded.Status)
		require.Equal(t, "outside role component scope", excluded.Reason)
		require.Empty(t, excluded.Steps)
	}
	require.Len(t, Steps(plans), 1)

	opts.Scope = nil
	_, err = Plan(root, cfg, opts)
	require.ErrorContains(t, err, "unavailable-tool")
	require.ErrorContains(t, err, "missing-directory")
}
