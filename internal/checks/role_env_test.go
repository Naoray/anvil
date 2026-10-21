package checks

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
)

func TestRoleUnsetAppliesToCommandsAndToolchainProbes(t *testing.T) {
	f := newFixture(t)
	t.Setenv("ANVIL_REVIEW_UNSET_TEST", "inherited")
	check := config.CheckConfig{
		Name:      "review-env",
		Commands:  cmds(`test -z "${ANVIL_REVIEW_UNSET_TEST-}"`),
		Toolchain: []string{`test -z "${ANVIL_REVIEW_UNSET_TEST-}"`},
	}
	req := f.request(check)
	req.Unset = []string{"ANVIL_REVIEW_UNSET_TEST"}
	result, err := Run(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, StatusPassed, result.Status)
	require.Len(t, result.Identity.Toolchain, 1)
	require.Zero(t, result.Identity.Toolchain[0].ExitCode, "review policy also applies to toolchain subprocesses")
}
