package steps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/artisanexperiences/arbor/internal/scaffold/template"
	"github.com/artisanexperiences/arbor/internal/scaffold/types"
)

func TestBashRunStep_TemplateReplacement(t *testing.T) {
	t.Run("name returns bash.run", func(t *testing.T) {
		step := NewBashRunStep("echo hello", "")
		assert.Equal(t, "bash.run", step.Name())
	})

	t.Run("condition always returns true", func(t *testing.T) {
		step := NewBashRunStep("echo hello", "")
		ctx := &types.ScaffoldContext{WorktreePath: "/tmp"}
		assert.True(t, step.Condition(ctx))
	})

	t.Run("replaces .SiteName in command", func(t *testing.T) {
		step := NewBashRunStep("echo 'Site: {{ .SiteName }}'", "")
		ctx := &types.ScaffoldContext{
			WorktreePath: "/tmp",
			SiteName:     "myapp",
		}

		replaced, err := step.replaceTemplateForTest(step.command, ctx)
		require.NoError(t, err)
		assert.Equal(t, "echo 'Site: myapp'", replaced)
	})

	t.Run("replaces .RepoName in command", func(t *testing.T) {
		step := NewBashRunStep("echo 'Repo: {{ .RepoName }}'", "")
		ctx := &types.ScaffoldContext{
			WorktreePath: "/tmp",
			RepoName:     "myrepo",
		}

		replaced, err := step.replaceTemplateForTest(step.command, ctx)
		require.NoError(t, err)
		assert.Equal(t, "echo 'Repo: myrepo'", replaced)
	})

	t.Run("replaces .Path in command", func(t *testing.T) {
		step := NewBashRunStep("echo 'Path: {{ .Path }}'", "")
		ctx := &types.ScaffoldContext{
			WorktreePath: "/tmp/myapp/feature-auth",
			Path:         "feature-auth",
		}

		replaced, err := step.replaceTemplateForTest(step.command, ctx)
		require.NoError(t, err)
		assert.Equal(t, "echo 'Path: feature-auth'", replaced)
	})

	t.Run("replaces .DbSuffix in command", func(t *testing.T) {
		step := NewBashRunStep("echo 'DB: myapp_{{ .DbSuffix }}'", "")
		ctx := &types.ScaffoldContext{
			WorktreePath: "/tmp",
		}
		ctx.SetDbSuffix("swift_runner")

		replaced, err := step.replaceTemplateForTest(step.command, ctx)
		require.NoError(t, err)
		assert.Equal(t, "echo 'DB: myapp_swift_runner'", replaced)
	})

	t.Run("replaces dynamic variables from context", func(t *testing.T) {
		step := NewBashRunStep("echo 'Custom: {{ .CustomVar }}'", "")
		ctx := &types.ScaffoldContext{
			WorktreePath: "/tmp",
		}
		ctx.SetVar("CustomVar", "custom_value")

		replaced, err := step.replaceTemplateForTest(step.command, ctx)
		require.NoError(t, err)
		assert.Equal(t, "echo 'Custom: custom_value'", replaced)
	})

	t.Run("handles whitespace variations in template syntax", func(t *testing.T) {
		step := NewBashRunStep("echo '{{ .SiteName }} {{ .RepoName }} {{ .Path }}'", "")
		ctx := &types.ScaffoldContext{
			WorktreePath: "/tmp",
			SiteName:     "myapp",
			RepoName:     "myrepo",
			Path:         "mypath",
		}

		replaced, err := step.replaceTemplateForTest(step.command, ctx)
		require.NoError(t, err)
		assert.Equal(t, "echo 'myapp myrepo mypath'", replaced)
	})

	t.Run("returns error for invalid template syntax", func(t *testing.T) {
		step := NewBashRunStep("echo '{{ invalid_syntax }'", "")
		ctx := &types.ScaffoldContext{
			WorktreePath: "/tmp",
			SiteName:     "myapp",
		}

		_, err := step.replaceTemplateForTest(step.command, ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid template")
	})
}

func TestBashRunStep_OutputCapture(t *testing.T) {
	t.Run("stores output in context when store_as is set", func(t *testing.T) {
		step := NewBashRunStep("echo 'hello world'", "Greeting")
		ctx := &types.ScaffoldContext{WorktreePath: t.TempDir()}

		err := step.Run(ctx, types.StepOptions{})

		assert.NoError(t, err)
		assert.Equal(t, "hello world", ctx.GetVar("Greeting"))
	})

	t.Run("trims whitespace from captured output", func(t *testing.T) {
		step := NewBashRunStep("echo '  spaced  '", "Spaced")
		ctx := &types.ScaffoldContext{WorktreePath: t.TempDir()}

		err := step.Run(ctx, types.StepOptions{})

		assert.NoError(t, err)
		assert.Equal(t, "spaced", ctx.GetVar("Spaced"))
	})

	t.Run("does not store output when store_as is empty", func(t *testing.T) {
		step := NewBashRunStep("echo 'hello'", "")
		ctx := &types.ScaffoldContext{WorktreePath: t.TempDir()}

		err := step.Run(ctx, types.StepOptions{})

		assert.NoError(t, err)
		// Context should be unchanged
		assert.Equal(t, "", ctx.GetVar("AnyVar"))
	})

	t.Run("does not store output on command failure", func(t *testing.T) {
		step := NewBashRunStep("echo 'error message' && exit 1", "ErrorMsg")
		ctx := &types.ScaffoldContext{WorktreePath: t.TempDir()}

		err := step.Run(ctx, types.StepOptions{})

		assert.Error(t, err)
		// Output should not be stored on failure
		assert.Equal(t, "", ctx.GetVar("ErrorMsg"))
	})

	t.Run("captures multiline output", func(t *testing.T) {
		step := NewBashRunStep("printf 'line1\\nline2\\nline3'", "Lines")
		ctx := &types.ScaffoldContext{WorktreePath: t.TempDir()}

		err := step.Run(ctx, types.StepOptions{})

		assert.NoError(t, err)
		assert.Equal(t, "line1\nline2\nline3", ctx.GetVar("Lines"))
	})

	t.Run("overwrites existing variable", func(t *testing.T) {
		step := NewBashRunStep("echo 'new value'", "MyVar")
		ctx := &types.ScaffoldContext{WorktreePath: t.TempDir()}
		ctx.SetVar("MyVar", "old value")

		err := step.Run(ctx, types.StepOptions{})

		assert.NoError(t, err)
		assert.Equal(t, "new value", ctx.GetVar("MyVar"))
	})
}

func (s *BashRunStep) replaceTemplateForTest(str string, ctx *types.ScaffoldContext) (string, error) {
	return s.templateReplaceForTest(str, ctx)
}

func (s *BashRunStep) templateReplaceForTest(str string, ctx *types.ScaffoldContext) (string, error) {
	return template.ReplaceTemplateVars(str, ctx)
}
