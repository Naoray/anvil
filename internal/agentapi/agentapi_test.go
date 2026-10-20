package agentapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvelopeSuccessAndFailure(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, NewEnvelope("ensure", true, map[string]string{"a": "<b>"}, nil, nil).Write(&out))
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &decoded))
	require.Equal(t, SchemaVersion, decoded["schema"])
	require.Equal(t, true, decoded["ok"])
	require.Equal(t, []any{}, decoded["warnings"], "warnings is always an array")
	require.NotContains(t, decoded, "error")
	require.Contains(t, out.String(), "<b>", "no HTML escaping")

	out.Reset()
	failure := Errorf(CodeWorktreeNotFound, "no worktree %q", "x").WithDetails(map[string]any{"query": "x"})
	wrapped := fmt.Errorf("context: %w", failure)
	require.NoError(t, NewEnvelope("status", false, nil, []string{"w"}, wrapped).Write(&out))
	require.NoError(t, json.Unmarshal(out.Bytes(), &decoded))
	require.Equal(t, false, decoded["ok"])
	body := decoded["error"].(map[string]any)
	require.Equal(t, CodeWorktreeNotFound, body["code"])
	require.Equal(t, ExitNotFound, failure.ExitCode())
}

func TestWrapKeepsClassificationAndDefaults(t *testing.T) {
	plain := errors.New("boom")
	require.Equal(t, CodeInternal, Wrap(CodeInternal, plain).Code)
	classified := Errorf(CodeLeaseHeld, "held")
	require.Same(t, classified, Wrap(CodeInternal, fmt.Errorf("x: %w", classified)))
	require.Equal(t, 42, (&Error{Code: CodeCheckFailed, Exit: 42}).ExitCode())
	require.Equal(t, ExitGeneral, (&Error{Code: "unknown"}).ExitCode())
	for code, exit := range Codes() {
		require.NotZero(t, exit, code)
	}
}
