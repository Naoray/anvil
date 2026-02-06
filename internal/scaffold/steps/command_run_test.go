package steps

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/naoray/anvil/internal/scaffold/types"
)

func TestCommandRunStep(t *testing.T) {
	t.Run("name returns command.run", func(t *testing.T) {
		step := NewCommandRunStep("echo hello", "")
		assert.Equal(t, "command.run", step.Name())
	})

	t.Run("condition always returns true", func(t *testing.T) {
		step := NewCommandRunStep("echo hello", "")
		ctx := &types.ScaffoldContext{WorktreePath: "/tmp"}
		assert.True(t, step.Condition(ctx))
	})

	t.Run("stores output in context when store_as is set", func(t *testing.T) {
		step := NewCommandRunStep("echo 'hello world'", "Greeting")
		ctx := &types.ScaffoldContext{WorktreePath: t.TempDir()}

		err := step.Run(ctx, types.StepOptions{})

		assert.NoError(t, err)
		assert.Equal(t, "hello world", ctx.GetVar("Greeting"))
	})

	t.Run("trims whitespace from captured output", func(t *testing.T) {
		step := NewCommandRunStep("echo '  spaced  '", "Spaced")
		ctx := &types.ScaffoldContext{WorktreePath: t.TempDir()}

		err := step.Run(ctx, types.StepOptions{})

		assert.NoError(t, err)
		assert.Equal(t, "spaced", ctx.GetVar("Spaced"))
	})

	t.Run("does not store output when store_as is empty", func(t *testing.T) {
		step := NewCommandRunStep("echo 'hello'", "")
		ctx := &types.ScaffoldContext{WorktreePath: t.TempDir()}

		err := step.Run(ctx, types.StepOptions{})

		assert.NoError(t, err)
		assert.Equal(t, "", ctx.GetVar("AnyVar"))
	})

	t.Run("does not store output on command failure", func(t *testing.T) {
		step := NewCommandRunStep("echo 'error message' && exit 1", "ErrorMsg")
		ctx := &types.ScaffoldContext{WorktreePath: t.TempDir()}

		err := step.Run(ctx, types.StepOptions{})

		assert.Error(t, err)
		assert.Equal(t, "", ctx.GetVar("ErrorMsg"))
	})

	t.Run("captures multiline output", func(t *testing.T) {
		step := NewCommandRunStep("printf 'line1\\nline2\\nline3'", "Lines")
		ctx := &types.ScaffoldContext{WorktreePath: t.TempDir()}

		err := step.Run(ctx, types.StepOptions{})

		assert.NoError(t, err)
		assert.Equal(t, "line1\nline2\nline3", ctx.GetVar("Lines"))
	})

	t.Run("overwrites existing variable", func(t *testing.T) {
		step := NewCommandRunStep("echo 'new value'", "MyVar")
		ctx := &types.ScaffoldContext{WorktreePath: t.TempDir()}
		ctx.SetVar("MyVar", "old value")

		err := step.Run(ctx, types.StepOptions{})

		assert.NoError(t, err)
		assert.Equal(t, "new value", ctx.GetVar("MyVar"))
	})
}
