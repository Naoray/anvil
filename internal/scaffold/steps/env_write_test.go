package steps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michaeldyrynda/arbor/internal/config"
	"github.com/michaeldyrynda/arbor/internal/scaffold/types"
)

func TestEnvWriteStep(t *testing.T) {
	t.Run("name returns env.write", func(t *testing.T) {
		step := NewEnvWriteStep(config.StepConfig{})
		assert.Equal(t, "env.write", step.Name())
	})

	t.Run("priority returns 0", func(t *testing.T) {
		step := NewEnvWriteStep(config.StepConfig{})
		assert.Equal(t, 0, step.Priority())
	})

	t.Run("condition always returns true", func(t *testing.T) {
		step := NewEnvWriteStep(config.StepConfig{})
		ctx := types.ScaffoldContext{WorktreePath: t.TempDir()}
		assert.True(t, step.Condition(ctx))
	})

	t.Run("creates new .env file with key=value", func(t *testing.T) {
		tmpDir := t.TempDir()

		step := NewEnvWriteStep(config.StepConfig{Key: "DB_DATABASE", Value: "test_db"})
		ctx := &types.ScaffoldContext{WorktreePath: tmpDir}

		err := step.Run(ctx, types.StepOptions{Verbose: false})

		assert.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(tmpDir, ".env"))
		require.NoError(t, err)
		assert.Equal(t, "DB_DATABASE=test_db\n", string(content))
	})

	t.Run("replaces existing key in .env file", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		require.NoError(t, os.WriteFile(envFile, []byte("DB_DATABASE=old_db\nAPP_NAME=myapp\n"), 0644))

		step := NewEnvWriteStep(config.StepConfig{Key: "DB_DATABASE", Value: "new_db"})
		ctx := &types.ScaffoldContext{WorktreePath: tmpDir}

		err := step.Run(ctx, types.StepOptions{Verbose: false})

		assert.NoError(t, err)

		content, err := os.ReadFile(envFile)
		require.NoError(t, err)
		lines := strings.Split(string(content), "\n")
		assert.Contains(t, lines[0], "DB_DATABASE=new_db")
		assert.Equal(t, "APP_NAME=myapp", lines[1])
	})

	t.Run("appends new key to end of .env file", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		require.NoError(t, os.WriteFile(envFile, []byte("APP_NAME=myapp\n"), 0644))

		step := NewEnvWriteStep(config.StepConfig{Key: "DB_DATABASE", Value: "test_db"})
		ctx := &types.ScaffoldContext{WorktreePath: tmpDir}

		err := step.Run(ctx, types.StepOptions{Verbose: false})

		assert.NoError(t, err)

		content, err := os.ReadFile(envFile)
		require.NoError(t, err)
		lines := strings.Split(string(content), "\n")
		assert.Equal(t, "APP_NAME=myapp", lines[0])
		assert.Equal(t, "DB_DATABASE=test_db", lines[1])
	})

	t.Run("writes to custom file path", func(t *testing.T) {
		tmpDir := t.TempDir()

		step := NewEnvWriteStep(config.StepConfig{Key: "DB_DATABASE", Value: "test_db", File: ".env.local"})
		ctx := &types.ScaffoldContext{WorktreePath: tmpDir}

		err := step.Run(ctx, types.StepOptions{Verbose: false})

		assert.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(tmpDir, ".env.local"))
		require.NoError(t, err)
		assert.Equal(t, "DB_DATABASE=test_db\n", string(content))
	})

	t.Run("preserves file permissions", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		require.NoError(t, os.WriteFile(envFile, []byte("APP_NAME=myapp\n"), 0600))

		step := NewEnvWriteStep(config.StepConfig{Key: "DB_DATABASE", Value: "test_db"})
		ctx := &types.ScaffoldContext{WorktreePath: tmpDir}

		err := step.Run(ctx, types.StepOptions{Verbose: false})

		assert.NoError(t, err)

		info, err := os.Stat(envFile)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
	})

	t.Run("replaces template variables in value", func(t *testing.T) {
		tmpDir := t.TempDir()

		step := NewEnvWriteStep(config.StepConfig{Key: "DB_DATABASE", Value: "{{ .SiteName }}_{{ .DbSuffix }}"})
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			SiteName:     "myapp",
		}
		ctx.SetDbSuffix("swift_runner")

		err := step.Run(ctx, types.StepOptions{Verbose: false})

		assert.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(tmpDir, ".env"))
		require.NoError(t, err)
		assert.Equal(t, "DB_DATABASE=myapp_swift_runner\n", string(content))
	})

	t.Run("preserves existing comments and ordering", func(t *testing.T) {
		tmpDir := t.TempDir()

		envContent := `# Database configuration
APP_ENV=local
# App name
APP_NAME=myapp
`
		envFile := filepath.Join(tmpDir, ".env")
		require.NoError(t, os.WriteFile(envFile, []byte(envContent), 0644))

		step := NewEnvWriteStep(config.StepConfig{Key: "DB_DATABASE", Value: "test_db"})
		ctx := &types.ScaffoldContext{WorktreePath: tmpDir}

		err := step.Run(ctx, types.StepOptions{Verbose: false})

		assert.NoError(t, err)

		content, err := os.ReadFile(envFile)
		require.NoError(t, err)
		assert.Contains(t, string(content), "# Database configuration")
		assert.Contains(t, string(content), "APP_ENV=local")
		assert.Contains(t, string(content), "# App name")
		assert.Contains(t, string(content), "APP_NAME=myapp")
		assert.Contains(t, string(content), "DB_DATABASE=test_db")
	})

	t.Run("ensures newline at end of file", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		require.NoError(t, os.WriteFile(envFile, []byte("APP_NAME=myapp\nDB_DATABASE=old_db"), 0644))

		step := NewEnvWriteStep(config.StepConfig{Key: "DB_DATABASE", Value: "new_db"})
		ctx := &types.ScaffoldContext{WorktreePath: tmpDir}

		err := step.Run(ctx, types.StepOptions{Verbose: false})

		assert.NoError(t, err)

		content, err := os.ReadFile(envFile)
		require.NoError(t, err)
		assert.True(t, strings.HasSuffix(string(content), "\n"))
	})

	t.Run("atomic write via temp file", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		require.NoError(t, os.WriteFile(envFile, []byte("APP_NAME=myapp\n"), 0644))

		step := NewEnvWriteStep(config.StepConfig{Key: "DB_DATABASE", Value: "test_db"})
		ctx := &types.ScaffoldContext{WorktreePath: tmpDir}

		err := step.Run(ctx, types.StepOptions{Verbose: false})

		assert.NoError(t, err)

		tmpFile := envFile + ".tmp"
		_, err = os.Stat(tmpFile)
		assert.True(t, os.IsNotExist(err), "temp file should be cleaned up")

		_, err = os.Stat(envFile)
		assert.NoError(t, err, "actual file should exist")
	})

	t.Run("replaces dynamic variables from context", func(t *testing.T) {
		tmpDir := t.TempDir()

		step := NewEnvWriteStep(config.StepConfig{Key: "APP_DOMAIN", Value: "app.{{ .Path }}.test"})
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			Path:         "feature-auth",
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})

		assert.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(tmpDir, ".env"))
		require.NoError(t, err)
		assert.Equal(t, "APP_DOMAIN=app.feature-auth.test\n", string(content))
	})
}
