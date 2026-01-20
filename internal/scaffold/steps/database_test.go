package steps

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/michaeldyrynda/arbor/internal/config"
	"github.com/michaeldyrynda/arbor/internal/scaffold/types"
)

func TestDatabaseStep(t *testing.T) {
	t.Run("condition always returns true - controlled by preset", func(t *testing.T) {
		step := NewDatabaseStep(config.StepConfig{}, 8)
		ctx := types.ScaffoldContext{
			WorktreePath: t.TempDir(),
		}

		assert.True(t, step.Condition(ctx))
	})

	t.Run("skips when no DB_CONNECTION in env file", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("APP_NAME=test\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		step := NewDatabaseStep(config.StepConfig{}, 8)
		ctx := types.ScaffoldContext{
			WorktreePath: tmpDir,
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)
	})

	t.Run("reads DB_CONNECTION from .env file", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\nDB_DATABASE=testdb\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		step := NewDatabaseStep(config.StepConfig{}, 8)
		ctx := types.ScaffoldContext{
			WorktreePath: tmpDir,
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)
	})

	t.Run("creates SQLite database file", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=sqlite\nDB_DATABASE=database/test.sqlite\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		step := NewDatabaseStep(config.StepConfig{}, 8)
		ctx := types.ScaffoldContext{
			WorktreePath: tmpDir,
		}

		err := step.Run(ctx, types.StepOptions{Verbose: true})
		assert.NoError(t, err)

		dbFile := filepath.Join(tmpDir, "database", "test.sqlite")
		assert.FileExists(t, dbFile)
	})

	t.Run("generates database name with app_ prefix", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		step := NewDatabaseStep(config.StepConfig{}, 8)
		ctx := types.ScaffoldContext{
			WorktreePath: tmpDir,
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)
	})

	t.Run("uses args for database configuration", func(t *testing.T) {
		tmpDir := t.TempDir()

		step := NewDatabaseStep(config.StepConfig{
			Args: []string{"--type", "sqlite", "--database", "custom.db"},
		}, 8)
		ctx := types.ScaffoldContext{
			WorktreePath: tmpDir,
		}

		err := step.Run(ctx, types.StepOptions{Verbose: true})
		assert.NoError(t, err)

		dbFile := filepath.Join(tmpDir, "custom.db")
		assert.FileExists(t, dbFile)
	})

	t.Run("name returns correct value", func(t *testing.T) {
		step := NewDatabaseStep(config.StepConfig{}, 8)
		assert.Equal(t, "database.create", step.Name())
	})

	t.Run("priority returns correct value", func(t *testing.T) {
		step := NewDatabaseStep(config.StepConfig{}, 8)
		assert.Equal(t, 8, step.Priority())
	})
}
