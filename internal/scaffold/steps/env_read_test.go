package steps

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/artisanexperiences/arbor/internal/config"
	"github.com/artisanexperiences/arbor/internal/scaffold/types"
)

func TestEnvReadStep(t *testing.T) {
	t.Run("name returns env.read", func(t *testing.T) {
		step := NewEnvReadStep(config.StepConfig{})
		assert.Equal(t, "env.read", step.Name())
	})

	t.Run("condition always returns true", func(t *testing.T) {
		step := NewEnvReadStep(config.StepConfig{})
		ctx := types.ScaffoldContext{WorktreePath: t.TempDir()}
		assert.True(t, step.Condition(&ctx))
	})

	t.Run("reads key from default .env file and stores as variable", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		require.NoError(t, os.WriteFile(envFile, []byte("DB_DATABASE=test_db\nAPP_NAME=myapp\n"), 0644))

		step := NewEnvReadStep(config.StepConfig{Key: "DB_DATABASE", StoreAs: "MyDatabase"})
		ctx := &types.ScaffoldContext{WorktreePath: tmpDir}

		err := step.Run(ctx, types.StepOptions{Verbose: false})

		assert.NoError(t, err)
		assert.Equal(t, "test_db", ctx.GetVar("MyDatabase"))
	})

	t.Run("uses key as variable name if store_as not specified", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		require.NoError(t, os.WriteFile(envFile, []byte("DB_DATABASE=test_db\n"), 0644))

		step := NewEnvReadStep(config.StepConfig{Key: "DB_DATABASE"})
		ctx := &types.ScaffoldContext{WorktreePath: tmpDir}

		err := step.Run(ctx, types.StepOptions{Verbose: false})

		assert.NoError(t, err)
		assert.Equal(t, "test_db", ctx.GetVar("DB_DATABASE"))
	})

	t.Run("reads from custom file path", func(t *testing.T) {
		tmpDir := t.TempDir()

		customFile := filepath.Join(tmpDir, ".env.local")
		require.NoError(t, os.WriteFile(customFile, []byte("DB_DATABASE=local_db\n"), 0644))

		step := NewEnvReadStep(config.StepConfig{Key: "DB_DATABASE", File: ".env.local"})
		ctx := &types.ScaffoldContext{WorktreePath: tmpDir}

		err := step.Run(ctx, types.StepOptions{Verbose: false})

		assert.NoError(t, err)
		assert.Equal(t, "local_db", ctx.GetVar("DB_DATABASE"))
	})

	t.Run("returns error if key not found", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		require.NoError(t, os.WriteFile(envFile, []byte("DB_DATABASE=test_db\n"), 0644))

		step := NewEnvReadStep(config.StepConfig{Key: "MISSING_KEY"})
		ctx := &types.ScaffoldContext{WorktreePath: tmpDir}

		err := step.Run(ctx, types.StepOptions{Verbose: false})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "key 'MISSING_KEY' not found")
	})

	t.Run("returns error if .env file does not exist", func(t *testing.T) {
		tmpDir := t.TempDir()

		step := NewEnvReadStep(config.StepConfig{Key: "DB_DATABASE"})
		ctx := &types.ScaffoldContext{WorktreePath: tmpDir}

		err := step.Run(ctx, types.StepOptions{Verbose: false})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "key 'DB_DATABASE' not found")
	})

	t.Run("handles values with special characters", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		require.NoError(t, os.WriteFile(envFile, []byte("DB_PASSWORD=p@ssw0rd!#$%\n"), 0644))

		step := NewEnvReadStep(config.StepConfig{Key: "DB_PASSWORD"})
		ctx := &types.ScaffoldContext{WorktreePath: tmpDir}

		err := step.Run(ctx, types.StepOptions{Verbose: false})

		assert.NoError(t, err)
		assert.Equal(t, "p@ssw0rd!#$%", ctx.GetVar("DB_PASSWORD"))
	})
}
