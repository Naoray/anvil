package steps

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/scaffold/types"
)

func TestEnvCopyStep(t *testing.T) {
	t.Run("name returns env.copy", func(t *testing.T) {
		step := NewEnvCopyStep(config.StepConfig{})
		assert.Equal(t, "env.copy", step.Name())
	})

	t.Run("condition always returns true", func(t *testing.T) {
		step := NewEnvCopyStep(config.StepConfig{})
		ctx := types.ScaffoldContext{WorktreePath: t.TempDir()}
		assert.True(t, step.Condition(&ctx))
	})

	t.Run("copies single key from source to target", func(t *testing.T) {
		sourceDir := t.TempDir()
		targetDir := t.TempDir()

		sourceEnv := filepath.Join(sourceDir, ".env")
		require.NoError(t, os.WriteFile(sourceEnv, []byte("API_KEY=secret123\nOTHER=value\n"), 0644))

		targetEnv := filepath.Join(targetDir, ".env")
		require.NoError(t, os.WriteFile(targetEnv, []byte("APP_NAME=myapp\n"), 0644))

		step := NewEnvCopyStep(config.StepConfig{
			Source: sourceDir,
			Key:    "API_KEY",
		})
		ctx := &types.ScaffoldContext{WorktreePath: targetDir}

		err := step.Run(ctx, types.StepOptions{Verbose: false})

		assert.NoError(t, err)
		content, _ := os.ReadFile(targetEnv)
		assert.Contains(t, string(content), "API_KEY=secret123")
		assert.Contains(t, string(content), "APP_NAME=myapp")
	})

	t.Run("copies multiple keys from source to target", func(t *testing.T) {
		sourceDir := t.TempDir()
		targetDir := t.TempDir()

		sourceEnv := filepath.Join(sourceDir, ".env")
		require.NoError(t, os.WriteFile(sourceEnv, []byte("API_KEY=secret123\nAPI_SECRET=secret456\nOTHER=value\n"), 0644))

		targetEnv := filepath.Join(targetDir, ".env")
		require.NoError(t, os.WriteFile(targetEnv, []byte("APP_NAME=myapp\n"), 0644))

		step := NewEnvCopyStep(config.StepConfig{
			Source: sourceDir,
			Keys:   []string{"API_KEY", "API_SECRET"},
		})
		ctx := &types.ScaffoldContext{WorktreePath: targetDir}

		err := step.Run(ctx, types.StepOptions{Verbose: false})

		assert.NoError(t, err)
		content, _ := os.ReadFile(targetEnv)
		assert.Contains(t, string(content), "API_KEY=secret123")
		assert.Contains(t, string(content), "API_SECRET=secret456")
		assert.Contains(t, string(content), "APP_NAME=myapp")
	})

	t.Run("creates target file if it does not exist", func(t *testing.T) {
		sourceDir := t.TempDir()
		targetDir := t.TempDir()

		sourceEnv := filepath.Join(sourceDir, ".env")
		require.NoError(t, os.WriteFile(sourceEnv, []byte("API_KEY=secret123\n"), 0644))

		step := NewEnvCopyStep(config.StepConfig{
			Source: sourceDir,
			Key:    "API_KEY",
		})
		ctx := &types.ScaffoldContext{WorktreePath: targetDir}

		err := step.Run(ctx, types.StepOptions{Verbose: false})

		assert.NoError(t, err)
		content, _ := os.ReadFile(filepath.Join(targetDir, ".env"))
		assert.Contains(t, string(content), "API_KEY=secret123")
	})

	t.Run("uses custom source and target files", func(t *testing.T) {
		sourceDir := t.TempDir()
		targetDir := t.TempDir()

		sourceEnv := filepath.Join(sourceDir, ".env.production")
		require.NoError(t, os.WriteFile(sourceEnv, []byte("API_KEY=prod_secret\n"), 0644))

		step := NewEnvCopyStep(config.StepConfig{
			Source:     sourceDir,
			SourceFile: ".env.production",
			File:       ".env.local",
			Key:        "API_KEY",
		})
		ctx := &types.ScaffoldContext{WorktreePath: targetDir}

		err := step.Run(ctx, types.StepOptions{Verbose: false})

		assert.NoError(t, err)
		content, _ := os.ReadFile(filepath.Join(targetDir, ".env.local"))
		assert.Contains(t, string(content), "API_KEY=prod_secret")
	})

	t.Run("updates existing key in target file", func(t *testing.T) {
		sourceDir := t.TempDir()
		targetDir := t.TempDir()

		sourceEnv := filepath.Join(sourceDir, ".env")
		require.NoError(t, os.WriteFile(sourceEnv, []byte("API_KEY=new_secret\n"), 0644))

		targetEnv := filepath.Join(targetDir, ".env")
		require.NoError(t, os.WriteFile(targetEnv, []byte("API_KEY=old_secret\nOTHER=value\n"), 0644))

		step := NewEnvCopyStep(config.StepConfig{
			Source: sourceDir,
			Key:    "API_KEY",
		})
		ctx := &types.ScaffoldContext{WorktreePath: targetDir}

		err := step.Run(ctx, types.StepOptions{Verbose: false})

		assert.NoError(t, err)
		content, _ := os.ReadFile(targetEnv)
		assert.Contains(t, string(content), "API_KEY=new_secret")
		assert.NotContains(t, string(content), "old_secret")
		assert.Contains(t, string(content), "OTHER=value")
	})

	t.Run("returns error if source file does not exist", func(t *testing.T) {
		sourceDir := t.TempDir()
		targetDir := t.TempDir()

		step := NewEnvCopyStep(config.StepConfig{
			Source: sourceDir,
			Key:    "API_KEY",
		})
		ctx := &types.ScaffoldContext{WorktreePath: targetDir}

		err := step.Run(ctx, types.StepOptions{Verbose: false})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "source file")
	})

	t.Run("returns error if key not found in source", func(t *testing.T) {
		sourceDir := t.TempDir()
		targetDir := t.TempDir()

		sourceEnv := filepath.Join(sourceDir, ".env")
		require.NoError(t, os.WriteFile(sourceEnv, []byte("OTHER=value\n"), 0644))

		step := NewEnvCopyStep(config.StepConfig{
			Source: sourceDir,
			Key:    "MISSING_KEY",
		})
		ctx := &types.ScaffoldContext{WorktreePath: targetDir}

		err := step.Run(ctx, types.StepOptions{Verbose: false})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "MISSING_KEY")
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("handles values with special characters", func(t *testing.T) {
		sourceDir := t.TempDir()
		targetDir := t.TempDir()

		sourceEnv := filepath.Join(sourceDir, ".env")
		require.NoError(t, os.WriteFile(sourceEnv, []byte("API_KEY=p@ssw0rd!#$%\n"), 0644))

		step := NewEnvCopyStep(config.StepConfig{
			Source: sourceDir,
			Key:    "API_KEY",
		})
		ctx := &types.ScaffoldContext{WorktreePath: targetDir}

		err := step.Run(ctx, types.StepOptions{Verbose: false})

		assert.NoError(t, err)
		content, _ := os.ReadFile(filepath.Join(targetDir, ".env"))
		assert.Contains(t, string(content), "API_KEY=p@ssw0rd!#$%")
	})

	t.Run("resolves relative source path from worktree", func(t *testing.T) {
		baseDir := t.TempDir()
		sourceDir := filepath.Join(baseDir, "main")
		targetDir := filepath.Join(baseDir, "feature-x")
		require.NoError(t, os.MkdirAll(sourceDir, 0755))
		require.NoError(t, os.MkdirAll(targetDir, 0755))

		sourceEnv := filepath.Join(sourceDir, ".env")
		require.NoError(t, os.WriteFile(sourceEnv, []byte("API_KEY=secret123\n"), 0644))

		step := NewEnvCopyStep(config.StepConfig{
			Source: "../main",
			Key:    "API_KEY",
		})
		ctx := &types.ScaffoldContext{WorktreePath: targetDir}

		err := step.Run(ctx, types.StepOptions{Verbose: false})

		assert.NoError(t, err)
		content, _ := os.ReadFile(filepath.Join(targetDir, ".env"))
		assert.Contains(t, string(content), "API_KEY=secret123")
	})

	t.Run("skips missing keys when copying multiple and some exist", func(t *testing.T) {
		sourceDir := t.TempDir()
		targetDir := t.TempDir()

		sourceEnv := filepath.Join(sourceDir, ".env")
		require.NoError(t, os.WriteFile(sourceEnv, []byte("API_KEY=secret123\n"), 0644))

		step := NewEnvCopyStep(config.StepConfig{
			Source: sourceDir,
			Keys:   []string{"API_KEY", "MISSING_KEY"},
		})
		ctx := &types.ScaffoldContext{WorktreePath: targetDir}

		err := step.Run(ctx, types.StepOptions{Verbose: false})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "MISSING_KEY")
	})
}
