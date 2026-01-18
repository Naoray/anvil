package steps

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/michaeldyrynda/arbor/internal/scaffold/types"
	"github.com/stretchr/testify/assert"
)

func TestDatabaseStep(t *testing.T) {
	t.Run("condition - returns true when DB_CONNECTION is set but DB_DATABASE is not", func(t *testing.T) {
		os.Setenv("DB_CONNECTION", "mysql")
		os.Unsetenv("DB_DATABASE")
		defer os.Unsetenv("DB_CONNECTION")

		step := NewDatabaseStep(8)
		ctx := types.ScaffoldContext{
			WorktreePath: t.TempDir(),
		}

		assert.True(t, step.Condition(ctx))
	})

	t.Run("condition - returns false when DB_CONNECTION is sqlite", func(t *testing.T) {
		os.Setenv("DB_CONNECTION", "sqlite")
		os.Unsetenv("DB_DATABASE")
		defer os.Unsetenv("DB_CONNECTION")

		step := NewDatabaseStep(8)
		ctx := types.ScaffoldContext{
			WorktreePath: t.TempDir(),
		}

		assert.False(t, step.Condition(ctx))
	})

	t.Run("condition - returns false when DB_DATABASE is already set", func(t *testing.T) {
		os.Setenv("DB_CONNECTION", "mysql")
		os.Setenv("DB_DATABASE", "existing_db")
		defer os.Unsetenv("DB_CONNECTION")
		defer os.Unsetenv("DB_DATABASE")

		step := NewDatabaseStep(8)
		ctx := types.ScaffoldContext{
			WorktreePath: t.TempDir(),
		}

		assert.False(t, step.Condition(ctx))
	})

	t.Run("condition - returns false when DB_CONNECTION is not set", func(t *testing.T) {
		os.Unsetenv("DB_CONNECTION")
		os.Unsetenv("DB_DATABASE")

		step := NewDatabaseStep(8)
		ctx := types.ScaffoldContext{
			WorktreePath: t.TempDir(),
		}

		assert.False(t, step.Condition(ctx))
	})

	t.Run("generates database name with app_ prefix", func(t *testing.T) {
		step := NewDatabaseStep(8)

		os.Setenv("DB_CONNECTION", "mysql")
		os.Unsetenv("DB_DATABASE")
		defer os.Unsetenv("DB_CONNECTION")
		defer os.Unsetenv("DB_DATABASE")

		ctx := types.ScaffoldContext{
			WorktreePath: t.TempDir(),
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)

		dbName := os.Getenv("DB_DATABASE")
		assert.Contains(t, dbName, "app_")
		assert.Len(t, dbName, 12) // "app_" + 8 hex chars = 12
	})

	t.Run("writes DB_DATABASE to .env file", func(t *testing.T) {
		step := NewDatabaseStep(8)

		tmpDir := t.TempDir()
		os.Setenv("DB_CONNECTION", "mysql")
		os.Unsetenv("DB_DATABASE")
		defer os.Unsetenv("DB_CONNECTION")
		defer os.Unsetenv("DB_DATABASE")

		envFile := filepath.Join(tmpDir, ".env")
		os.WriteFile(envFile, []byte("APP_NAME=test\n"), 0644)

		ctx := types.ScaffoldContext{
			WorktreePath: tmpDir,
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)

		content, err := os.ReadFile(envFile)
		assert.NoError(t, err)
		assert.Contains(t, string(content), "DB_DATABASE=")
	})

	t.Run("name returns correct value", func(t *testing.T) {
		step := NewDatabaseStep(8)
		assert.Equal(t, "database.create", step.Name())
	})

	t.Run("priority returns correct value", func(t *testing.T) {
		step := NewDatabaseStep(8)
		assert.Equal(t, 8, step.Priority())
	})
}
