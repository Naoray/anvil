package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContextVarPresenceAndLiteralValues(t *testing.T) {
	for _, tc := range []struct {
		name         string
		present      bool
		actual, want string
		match        bool
	}{
		{name: "absent is not empty", want: ""},
		{name: "present empty", present: true, match: true},
		{name: "true", present: true, actual: "true", want: "true", match: true},
		{name: "false", present: true, actual: "false", want: "false", match: true},
		{name: "false is not truthy", present: true, actual: "false", want: "true"},
		{name: "literal case", present: true, actual: "True", want: "true"},
		{name: "literal whitespace", present: true, actual: "true ", want: "true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := &ScaffoldContext{}
			if tc.present {
				ctx.SetVar("Decision", tc.actual)
			}
			condition := map[string]any{"context_var": map[string]any{"name": "Decision", "value": tc.want}}
			match, err := ctx.EvaluateCondition(condition)
			require.NoError(t, err)
			require.Equal(t, tc.match, match)
		})
	}
}

func TestConditionValidationRejectsMalformedOperands(t *testing.T) {
	for _, condition := range []map[string]any{
		{"unknown": true},
		{"context_var": "Decision"},
		{"context_var": map[string]any{"name": "Decision"}},
		{"context_var": map[string]any{"name": "", "value": "true"}},
		{"context_var": map[string]any{"name": "Decision", "value": true}},
		{"context_var": map[string]any{"name": "Decision", "value": "true", "extra": "x"}},
		{"not": true},
		{"not": []any{true}},
		{"not": []any{map[string]any{"file_exists": "absent"}, map[string]any{"unknown": true}}},
		{"file_exists": []any{"file", false}},
		{"file_exists": map[string]any{"file": "a", "typo": "b"}},
		{"file_contains": map[string]any{"file": "file"}},
		{"file_has_script": false},
		{"command_exists": nil},
		{"os": map[string]any{"name": "linux"}},
		{"env_exists": []any{nil}},
		{"env_not_exists": 10},
		{"env_file_contains": map[string]any{"key": "A"}},
		{"env_file_missing": map[string]any{"file": ".env", "key": false}},
	} {
		t.Run("invalid", func(t *testing.T) {
			ctx := &ScaffoldContext{}
			first := ValidateConditions(condition)
			require.Error(t, first)
			for i := 0; i < 10; i++ {
				require.EqualError(t, ValidateConditions(condition), first.Error())
			}
			match, err := ctx.EvaluateCondition(condition)
			require.Error(t, err)
			require.False(t, match)
		})
	}
}

func TestNestedNegationConjoinsWithSiblings(t *testing.T) {
	ctx := &ScaffoldContext{}
	ctx.SetVar("Ready", "true")
	condition := map[string]any{
		"not":         map[string]any{"context_var": map[string]any{"name": "Absent", "value": ""}},
		"context_var": map[string]any{"name": "Ready", "value": "false"},
	}
	match, err := ctx.EvaluateCondition(condition)
	require.NoError(t, err)
	require.False(t, match)
	condition["not"] = []any{
		map[string]any{"not": map[string]any{"context_var": map[string]any{"name": "Ready", "value": "false"}}},
		map[string]any{"context_var": map[string]any{"name": "Ready", "value": "true"}},
	}
	condition["context_var"] = map[string]any{"name": "Ready", "value": "true"}
	match, err = ctx.EvaluateCondition(condition)
	require.NoError(t, err)
	require.False(t, match)
	condition["not"] = map[string]any{"not": map[string]any{"context_var": map[string]any{"name": "Ready", "value": "true"}}}
	match, err = ctx.EvaluateCondition(condition)
	require.NoError(t, err)
	require.True(t, match)
}

func TestValidationChecksEveryOperandBeforeEvaluation(t *testing.T) {
	ctx := &ScaffoldContext{}
	match, err := ctx.EvaluateCondition(map[string]any{"file_exists": "missing", "zzz_typo": true})
	require.False(t, match)
	require.ErrorContains(t, err, "zzz_typo")
	require.NoError(t, ValidateConditions(map[string]any{"context_var": map[string]any{"name": "NotProducedYet", "value": "true"}}))
}
