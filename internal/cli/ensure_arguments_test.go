package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureArgumentErrorsHaveJSONEnvelope(t *testing.T) {
	f := newAgentFixture(t, ensureFixtureYAML, nil)
	for _, args := range [][]string{
		{"ensure", "a", "b"},
		{"ensure", "--unknown-flag"},
		{"ensure", "--verbose", "--quiet"},
		{"ensure", "--refresh=invalid"},
	} {
		envelope, stderr, code := f.runJSON(f.repo, args...)
		require.Equal(t, 2, code, stderr)
		require.Equal(t, false, envelope["ok"])
		require.Equal(t, "ensure", envelope["command"])
		require.Equal(t, "invalid_arguments", field(t, envelope, "error", "code"))
	}
	// A JSON request parsed before the error has the same boundary.
	envelope, stderr, code := f.runJSON(f.repo, "ensure", "--json", "--unknown-flag")
	require.Equal(t, 2, code, stderr)
	require.Equal(t, "invalid_arguments", field(t, envelope, "error", "code"))
}
