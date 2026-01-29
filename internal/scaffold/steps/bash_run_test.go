package steps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michaeldyrynda/arbor/internal/scaffold/template"
	"github.com/michaeldyrynda/arbor/internal/scaffold/types"
)

func TestBashRunStep_TemplateReplacement(t *testing.T) {
	t.Run("name returns bash.run", func(t *testing.T) {
		step := NewBashRunStep("echo hello")
		assert.Equal(t, "bash.run", step.Name())
	})

	t.Run("condition always returns true", func(t *testing.T) {
		step := NewBashRunStep("echo hello")
		ctx := &types.ScaffoldContext{WorktreePath: "/tmp"}
		assert.True(t, step.Condition(ctx))
	})

	t.Run("replaces .SiteName in command", func(t *testing.T) {
		step := NewBashRunStep("echo 'Site: {{ .SiteName }}'")
		ctx := &types.ScaffoldContext{
			WorktreePath: "/tmp",
			SiteName:     "myapp",
		}

		replaced, err := step.replaceTemplateForTest(step.command, ctx)
		require.NoError(t, err)
		assert.Equal(t, "echo 'Site: myapp'", replaced)
	})

	t.Run("replaces .RepoName in command", func(t *testing.T) {
		step := NewBashRunStep("echo 'Repo: {{ .RepoName }}'")
		ctx := &types.ScaffoldContext{
			WorktreePath: "/tmp",
			RepoName:     "myrepo",
		}

		replaced, err := step.replaceTemplateForTest(step.command, ctx)
		require.NoError(t, err)
		assert.Equal(t, "echo 'Repo: myrepo'", replaced)
	})

	t.Run("replaces .Path in command", func(t *testing.T) {
		step := NewBashRunStep("echo 'Path: {{ .Path }}'")
		ctx := &types.ScaffoldContext{
			WorktreePath: "/tmp/myapp/feature-auth",
			Path:         "feature-auth",
		}

		replaced, err := step.replaceTemplateForTest(step.command, ctx)
		require.NoError(t, err)
		assert.Equal(t, "echo 'Path: feature-auth'", replaced)
	})

	t.Run("replaces .DbSuffix in command", func(t *testing.T) {
		step := NewBashRunStep("echo 'DB: myapp_{{ .DbSuffix }}'")
		ctx := &types.ScaffoldContext{
			WorktreePath: "/tmp",
		}
		ctx.SetDbSuffix("swift_runner")

		replaced, err := step.replaceTemplateForTest(step.command, ctx)
		require.NoError(t, err)
		assert.Equal(t, "echo 'DB: myapp_swift_runner'", replaced)
	})

	t.Run("replaces dynamic variables from context", func(t *testing.T) {
		step := NewBashRunStep("echo 'Custom: {{ .CustomVar }}'")
		ctx := &types.ScaffoldContext{
			WorktreePath: "/tmp",
		}
		ctx.SetVar("CustomVar", "custom_value")

		replaced, err := step.replaceTemplateForTest(step.command, ctx)
		require.NoError(t, err)
		assert.Equal(t, "echo 'Custom: custom_value'", replaced)
	})

	t.Run("handles whitespace variations in template syntax", func(t *testing.T) {
		step := NewBashRunStep("echo '{{ .SiteName }} {{ .RepoName }} {{ .Path }}'")
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
		step := NewBashRunStep("echo '{{ invalid_syntax }'")
		ctx := &types.ScaffoldContext{
			WorktreePath: "/tmp",
			SiteName:     "myapp",
		}

		_, err := step.replaceTemplateForTest(step.command, ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid template")
	})
}

func (s *BashRunStep) replaceTemplateForTest(str string, ctx *types.ScaffoldContext) (string, error) {
	return s.templateReplaceForTest(str, ctx)
}

func (s *BashRunStep) templateReplaceForTest(str string, ctx *types.ScaffoldContext) (string, error) {
	return template.ReplaceTemplateVars(str, ctx)
}
