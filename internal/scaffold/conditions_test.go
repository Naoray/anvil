package scaffold

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/michaeldyrynda/arbor/internal/scaffold/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConditionEvaluator_Evaluate(t *testing.T) {
	tmpDir := t.TempDir()

	ctx := types.ScaffoldContext{
		WorktreePath: tmpDir,
		Branch:       "test-branch",
		Preset:       "php",
		Env:          make(map[string]string),
	}

	evaluator := NewConditionEvaluator(ctx)

	t.Run("empty conditions returns true", func(t *testing.T) {
		result, err := evaluator.Evaluate(map[string]interface{}{})
		assert.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("nil conditions returns true", func(t *testing.T) {
		result, err := evaluator.Evaluate(nil)
		assert.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("file_exists - file exists", func(t *testing.T) {
		os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("test"), 0644)

		result, err := evaluator.Evaluate(map[string]interface{}{
			"file_exists": "test.txt",
		})
		assert.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("file_exists - file does not exist", func(t *testing.T) {
		result, err := evaluator.Evaluate(map[string]interface{}{
			"file_exists": "nonexistent.txt",
		})
		assert.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("file_contains - file contains pattern", func(t *testing.T) {
		os.WriteFile(filepath.Join(tmpDir, "composer.json"), []byte(`{"name": "test/package"}`), 0644)

		result, err := evaluator.Evaluate(map[string]interface{}{
			"file_contains": map[string]interface{}{
				"file":    "composer.json",
				"pattern": "test/package",
			},
		})
		assert.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("file_contains - file does not contain pattern", func(t *testing.T) {
		os.WriteFile(filepath.Join(tmpDir, "composer.json"), []byte(`{"name": "other/package"}`), 0644)

		result, err := evaluator.Evaluate(map[string]interface{}{
			"file_contains": map[string]interface{}{
				"file":    "composer.json",
				"pattern": "missing/pattern",
			},
		})
		assert.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("command_exists - command exists", func(t *testing.T) {
		result, err := evaluator.Evaluate(map[string]interface{}{
			"command_exists": "go",
		})
		assert.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("command_exists - command does not exist", func(t *testing.T) {
		result, err := evaluator.Evaluate(map[string]interface{}{
			"command_exists": "nonexistentcommand123",
		})
		assert.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("os matches current OS", func(t *testing.T) {
		result, err := evaluator.Evaluate(map[string]interface{}{
			"os": "darwin",
		})
		assert.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("os does not match current OS", func(t *testing.T) {
		result, err := evaluator.Evaluate(map[string]interface{}{
			"os": "linux",
		})
		assert.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("env_exists - env variable exists", func(t *testing.T) {
		t.Setenv("TEST_VAR", "value")

		result, err := evaluator.Evaluate(map[string]interface{}{
			"env_exists": "TEST_VAR",
		})
		assert.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("env_exists - env variable does not exist", func(t *testing.T) {
		result, err := evaluator.Evaluate(map[string]interface{}{
			"env_exists": "NONEXISTENT_VAR_123",
		})
		assert.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("not condition - negates true condition", func(t *testing.T) {
		os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("test"), 0644)

		result, err := evaluator.Evaluate(map[string]interface{}{
			"not": map[string]interface{}{
				"file_exists": "test.txt",
			},
		})
		assert.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("not condition - negates false condition", func(t *testing.T) {
		result, err := evaluator.Evaluate(map[string]interface{}{
			"not": map[string]interface{}{
				"file_exists": "nonexistent.txt",
			},
		})
		assert.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("multiple conditions - all true", func(t *testing.T) {
		os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("test"), 0644)

		result, err := evaluator.Evaluate(map[string]interface{}{
			"file_exists":    "test.txt",
			"command_exists": "go",
		})
		assert.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("multiple conditions - one false", func(t *testing.T) {
		os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("test"), 0644)

		result, err := evaluator.Evaluate(map[string]interface{}{
			"file_exists":    "test.txt",
			"command_exists": "nonexistentcommand123",
		})
		assert.NoError(t, err)
		assert.False(t, result)
	})
}

func TestConditionEvaluator_fileHasScript(t *testing.T) {
	tmpDir := t.TempDir()

	ctx := types.ScaffoldContext{
		WorktreePath: tmpDir,
		Branch:       "test-branch",
		Preset:       "php",
		Env:          make(map[string]string),
	}

	evaluator := NewConditionEvaluator(ctx)

	t.Run("package.json has script", func(t *testing.T) {
		os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(`{"scripts": {"build": "vite build"}}`), 0644)

		result, err := evaluator.fileHasScript("build")
		require.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("package.json does not have script", func(t *testing.T) {
		os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(`{"scripts": {"test": "jest"}}`), 0644)

		result, err := evaluator.fileHasScript("build")
		require.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("package.json does not exist", func(t *testing.T) {
		result, err := evaluator.fileHasScript("build")
		require.NoError(t, err)
		assert.False(t, result)
	})
}
