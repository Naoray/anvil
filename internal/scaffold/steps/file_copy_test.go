package steps

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/michaeldyrynda/arbor/internal/scaffold/types"
)

func TestFileCopyStep(t *testing.T) {
	t.Run("copies file from source to destination", func(t *testing.T) {
		tmpDir := t.TempDir()

		fromFile := filepath.Join(tmpDir, "source.txt")
		toFile := filepath.Join(tmpDir, "destination.txt")
		content := []byte("test content")

		err := os.WriteFile(fromFile, content, 0644)
		assert.NoError(t, err)

		step := NewFileCopyStep("source.txt", "destination.txt")
		ctx := types.ScaffoldContext{
			WorktreePath: tmpDir,
		}

		err = step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)

		result, err := os.ReadFile(toFile)
		assert.NoError(t, err)
		assert.Equal(t, content, result)
	})

	t.Run("verbose output shows copying action", func(t *testing.T) {
		tmpDir := t.TempDir()

		fromFile := filepath.Join(tmpDir, ".env.example")
		toFile := filepath.Join(tmpDir, ".env")

		err := os.WriteFile(fromFile, []byte("APP_KEY=\n"), 0644)
		assert.NoError(t, err)

		step := NewFileCopyStep(".env.example", ".env")
		ctx := types.ScaffoldContext{
			WorktreePath: tmpDir,
		}

		err = step.Run(ctx, types.StepOptions{Verbose: true})
		assert.NoError(t, err)

		assert.FileExists(t, toFile)
	})

	t.Run("condition returns true when source file exists", func(t *testing.T) {
		tmpDir := t.TempDir()

		fromFile := filepath.Join(tmpDir, "source.txt")
		err := os.WriteFile(fromFile, []byte("test"), 0644)
		assert.NoError(t, err)

		step := NewFileCopyStep("source.txt", "destination.txt")
		ctx := types.ScaffoldContext{
			WorktreePath: tmpDir,
		}

		assert.True(t, step.Condition(ctx))
	})

	t.Run("condition returns false when source file does not exist", func(t *testing.T) {
		tmpDir := t.TempDir()

		step := NewFileCopyStep("nonexistent.txt", "destination.txt")
		ctx := types.ScaffoldContext{
			WorktreePath: tmpDir,
		}

		assert.False(t, step.Condition(ctx))
	})

	t.Run("returns error when source file does not exist", func(t *testing.T) {
		tmpDir := t.TempDir()

		step := NewFileCopyStep("nonexistent.txt", "destination.txt")
		ctx := types.ScaffoldContext{
			WorktreePath: tmpDir,
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.Error(t, err)
	})

	t.Run("name returns correct value", func(t *testing.T) {
		step := NewFileCopyStep("from", "to")
		assert.Equal(t, "file.copy", step.Name())
	})

	t.Run("priority returns correct value", func(t *testing.T) {
		step := NewFileCopyStep("from", "to", 25)
		assert.Equal(t, 25, step.Priority())
	})

	t.Run("default priority is 15", func(t *testing.T) {
		step := NewFileCopyStep("from", "to")
		assert.Equal(t, 15, step.Priority())
	})
}
