package steps

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/scaffold/types"
)

func TestBinaryStep_CommandConstruction(t *testing.T) {
	t.Run("php.composer with install", func(t *testing.T) {
		step, err := Create("php.composer", config.StepConfig{
			Args: []string{"install"},
		})
		require.NoError(t, err)
		assert.Equal(t, "php.composer", step.Name())

		binaryStep, ok := step.(*BinaryStep)
		assert.True(t, ok, "Expected BinaryStep type")
		assert.Equal(t, "composer", binaryStep.binary)
		assert.Equal(t, []string{"install"}, binaryStep.args)
	})

	t.Run("php binary", func(t *testing.T) {
		step, err := Create("php", config.StepConfig{
			Args: []string{"-v"},
		})
		require.NoError(t, err)
		assert.Equal(t, "php", step.Name())

		binaryStep, ok := step.(*BinaryStep)
		assert.True(t, ok, "Expected BinaryStep type")
		assert.Equal(t, "php", binaryStep.binary)
		assert.Equal(t, []string{"-v"}, binaryStep.args)
	})

	t.Run("php.laravel uses BinaryStep with 'php artisan' binary", func(t *testing.T) {
		step, err := Create("php.laravel", config.StepConfig{
			Args: []string{"key:generate", "--no-interaction"},
		})
		require.NoError(t, err)
		assert.Equal(t, "php.laravel", step.Name())

		binaryStep, ok := step.(*BinaryStep)
		assert.True(t, ok, "Expected BinaryStep type")
		assert.Equal(t, "php artisan", binaryStep.binary)
		assert.Equal(t, []string{"key:generate", "--no-interaction"}, binaryStep.args)
	})
}

func TestBinaryStep_CommandConstructionChecks(t *testing.T) {
	t.Run("php.composer command construction", func(t *testing.T) {
		step, err := Create("php.composer", config.StepConfig{
			Args: []string{"install", "--no-interaction"},
		})
		require.NoError(t, err)

		binaryStep, ok := step.(*BinaryStep)
		assert.True(t, ok, "Expected BinaryStep type")
		assert.Equal(t, "composer", binaryStep.binary)

		allArgs := append(strings.Fields(binaryStep.binary), binaryStep.args...)
		expectedCommand := "composer install --no-interaction"
		assert.Equal(t, expectedCommand, joinArgs(allArgs))
	})

	t.Run("php command construction", func(t *testing.T) {
		step, err := Create("php", config.StepConfig{
			Args: []string{"-v"},
		})
		require.NoError(t, err)

		binaryStep, ok := step.(*BinaryStep)
		assert.True(t, ok, "Expected BinaryStep type")
		assert.Equal(t, "php", binaryStep.binary)

		allArgs := append(strings.Fields(binaryStep.binary), binaryStep.args...)
		expectedCommand := "php -v"
		assert.Equal(t, expectedCommand, joinArgs(allArgs))
	})

	t.Run("php.laravel command construction", func(t *testing.T) {
		step, err := Create("php.laravel", config.StepConfig{
			Args: []string{"key:generate", "--no-interaction"},
		})
		require.NoError(t, err)

		binaryStep, ok := step.(*BinaryStep)
		assert.True(t, ok, "Expected BinaryStep type")
		assert.Equal(t, "php artisan", binaryStep.binary)

		allArgs := append(strings.Fields(binaryStep.binary), binaryStep.args...)
		expectedCommand := "php artisan key:generate --no-interaction"
		assert.Equal(t, expectedCommand, joinArgs(allArgs))
	})

	t.Run("php.laravel migrate:fresh command", func(t *testing.T) {
		step, err := Create("php.laravel", config.StepConfig{
			Args: []string{"migrate:fresh", "--seed", "--no-interaction"},
		})
		require.NoError(t, err)

		binaryStep, ok := step.(*BinaryStep)
		assert.True(t, ok, "Expected BinaryStep type")

		allArgs := append(strings.Fields(binaryStep.binary), binaryStep.args...)
		expectedCommand := "php artisan migrate:fresh --seed --no-interaction"
		assert.Equal(t, expectedCommand, joinArgs(allArgs))
	})

	t.Run("binary step condition checks first part of multi-part binary", func(t *testing.T) {
		step, err := Create("php.laravel", config.StepConfig{
			Args: []string{"storage:link"},
		})
		require.NoError(t, err)

		_, ok := step.(*BinaryStep)
		assert.True(t, ok, "Expected BinaryStep type")

		_, err = exec.LookPath("php")
		hasPHP := err == nil

		ctx := types.ScaffoldContext{
			WorktreePath: "/tmp",
		}

		result := step.Condition(&ctx)
		assert.Equal(t, hasPHP, result, "Condition should check if 'php' exists")
	})
}

func joinArgs(args []string) string {
	result := ""
	for i, arg := range args {
		if i > 0 {
			result += " "
		}
		result += arg
	}
	return result
}

func TestConditionEvaluator_viaContext(t *testing.T) {
	tmpDir := t.TempDir()

	ctx := &types.ScaffoldContext{
		WorktreePath: tmpDir,
		Branch:       "test-branch",
		Preset:       "php",
		Env:          make(map[string]string),
	}

	t.Run("empty conditions returns true", func(t *testing.T) {
		result, err := ctx.EvaluateCondition(map[string]interface{}{})
		assert.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("nil conditions returns true", func(t *testing.T) {
		result, err := ctx.EvaluateCondition(nil)
		assert.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("file_exists - file exists", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("test"), 0644); err != nil {
			t.Fatalf("writing test file: %v", err)
		}

		result, err := ctx.EvaluateCondition(map[string]interface{}{
			"file_exists": "test.txt",
		})
		assert.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("file_exists - file does not exist", func(t *testing.T) {
		result, err := ctx.EvaluateCondition(map[string]interface{}{
			"file_exists": "nonexistent.txt",
		})
		assert.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("file_contains - file contains pattern", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(tmpDir, "composer.json"), []byte(`{"name": "test/package"}`), 0644); err != nil {
			t.Fatalf("writing composer.json: %v", err)
		}

		result, err := ctx.EvaluateCondition(map[string]interface{}{
			"file_contains": map[string]interface{}{
				"file":    "composer.json",
				"pattern": "test/package",
			},
		})
		assert.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("file_contains - file does not contain pattern", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "composer.json"), []byte(`{"name": "other/package"}`), 0644))

		result, err := ctx.EvaluateCondition(map[string]interface{}{
			"file_contains": map[string]interface{}{
				"file":    "composer.json",
				"pattern": "missing/pattern",
			},
		})
		assert.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("command_exists - command exists", func(t *testing.T) {
		result, err := ctx.EvaluateCondition(map[string]interface{}{
			"command_exists": "go",
		})
		assert.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("command_exists - command does not exist", func(t *testing.T) {
		result, err := ctx.EvaluateCondition(map[string]interface{}{
			"command_exists": "nonexistentcommand123",
		})
		assert.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("os matches current OS", func(t *testing.T) {
		result, err := ctx.EvaluateCondition(map[string]interface{}{
			"os": runtime.GOOS,
		})
		assert.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("os does not match current OS", func(t *testing.T) {
		var otherOS string
		switch runtime.GOOS {
		case "darwin":
			otherOS = "linux"
		case "linux":
			otherOS = "darwin"
		case "windows":
			otherOS = "linux"
		default:
			otherOS = "windows"
		}
		result, err := ctx.EvaluateCondition(map[string]interface{}{
			"os": otherOS,
		})
		assert.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("env_exists - env variable exists", func(t *testing.T) {
		t.Setenv("TEST_VAR_STEP", "value")

		result, err := ctx.EvaluateCondition(map[string]interface{}{
			"env_exists": "TEST_VAR_STEP",
		})
		assert.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("env_exists - env variable does not exist", func(t *testing.T) {
		result, err := ctx.EvaluateCondition(map[string]interface{}{
			"env_exists": "NONEXISTENT_VAR_456",
		})
		assert.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("env_not_exists - env variable does not exist", func(t *testing.T) {
		result, err := ctx.EvaluateCondition(map[string]interface{}{
			"env_not_exists": "NONEXISTENT_VAR_789",
		})
		assert.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("env_not_exists - env variable exists", func(t *testing.T) {
		t.Setenv("TEST_VAR_STEP_EXISTS", "value")

		result, err := ctx.EvaluateCondition(map[string]interface{}{
			"env_not_exists": "TEST_VAR_STEP_EXISTS",
		})
		assert.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("env_file_contains - key exists in .env file", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("DB_CONNECTION=sqlite\nAPP_KEY=base64:value\n"), 0644))

		result, err := ctx.EvaluateCondition(map[string]interface{}{
			"env_file_contains": map[string]interface{}{
				"file": ".env",
				"key":  "DB_CONNECTION",
			},
		})
		assert.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("env_file_contains - key does not exist in .env file", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("APP_KEY=base64:value\n"), 0644))

		result, err := ctx.EvaluateCondition(map[string]interface{}{
			"env_file_contains": map[string]interface{}{
				"file": ".env",
				"key":  "DB_CONNECTION",
			},
		})
		assert.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("env_file_contains - .env file does not exist", func(t *testing.T) {
		result, err := ctx.EvaluateCondition(map[string]interface{}{
			"env_file_contains": map[string]interface{}{
				"file": ".env",
				"key":  "DB_CONNECTION",
			},
		})
		assert.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("env_file_missing - .env file does not exist", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("OTHER_KEY=other_value\n"), 0644))

		result, err := ctx.EvaluateCondition(map[string]interface{}{
			"env_file_missing": map[string]interface{}{
				"file": ".env",
				"key":  "APP_KEY",
			},
		})
		assert.NoError(t, err)
		assert.True(t, result, "APP_KEY should be missing from .env file")
	})

	t.Run("env_file_missing - key does not exist", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("APP_KEY=base64:value\n"), 0644))

		result, err := ctx.EvaluateCondition(map[string]interface{}{
			"env_file_missing": map[string]interface{}{
				"file": ".env",
				"key":  "DB_CONNECTION",
			},
		})
		assert.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("env_file_missing - key exists with value", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("APP_KEY=base64:value\n"), 0644))

		result, err := ctx.EvaluateCondition(map[string]interface{}{
			"env_file_missing": map[string]interface{}{
				"file": ".env",
				"key":  "APP_KEY",
			},
		})
		assert.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("not condition - negates true condition", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("test"), 0644))

		result, err := ctx.EvaluateCondition(map[string]interface{}{
			"not": map[string]interface{}{
				"file_exists": "test.txt",
			},
		})
		assert.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("not condition - negates false condition", func(t *testing.T) {
		result, err := ctx.EvaluateCondition(map[string]interface{}{
			"not": map[string]interface{}{
				"file_exists": "nonexistent.txt",
			},
		})
		assert.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("multiple conditions - all true", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("test"), 0644))

		result, err := ctx.EvaluateCondition(map[string]interface{}{
			"file_exists":    "test.txt",
			"command_exists": "go",
		})
		assert.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("multiple conditions - one false", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("test"), 0644))

		result, err := ctx.EvaluateCondition(map[string]interface{}{
			"file_exists":    "test.txt",
			"command_exists": "nonexistentcommand123",
		})
		assert.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("array condition via not - all conditions true", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("test"), 0644))

		result, err := ctx.EvaluateCondition(map[string]interface{}{
			"not": []interface{}{
				map[string]interface{}{"file_exists": "test.txt"},
				map[string]interface{}{"command_exists": "go"},
			},
		})
		assert.NoError(t, err)
		assert.False(t, result)
	})
}

func TestBinaryStep_TemplateReplacement(t *testing.T) {
	t.Run("replaces .SiteName in args", func(t *testing.T) {
		step, err := Create("php.composer", config.StepConfig{
			Args: []string{"install", "--name={{ .SiteName }}"},
		})
		require.NoError(t, err)
		binaryStep, ok := step.(*BinaryStep)
		require.True(t, ok, "Expected BinaryStep type")

		ctx := &types.ScaffoldContext{
			WorktreePath: "/tmp",
			SiteName:     "myapp",
		}

		replacedArgs := binaryStep.replaceTemplate(binaryStep.args, ctx)
		assert.Contains(t, replacedArgs, "--name=myapp")
	})

	t.Run("replaces .RepoName in args", func(t *testing.T) {
		step, err := Create("php.composer", config.StepConfig{
			Args: []string{"install", "--repo={{ .RepoName }}"},
		})
		require.NoError(t, err)
		binaryStep, ok := step.(*BinaryStep)
		require.True(t, ok, "Expected BinaryStep type")

		ctx := &types.ScaffoldContext{
			WorktreePath: "/tmp",
			RepoName:     "myrepo",
		}

		replacedArgs := binaryStep.replaceTemplate(binaryStep.args, ctx)
		assert.Contains(t, replacedArgs, "--repo=myrepo")
	})

	t.Run("replaces .Path in args", func(t *testing.T) {
		step, err := Create("herd", config.StepConfig{
			Args: []string{"link", "{{ .SiteName }}", "--domain={{ .Path }}.test"},
		})
		require.NoError(t, err)
		binaryStep, ok := step.(*BinaryStep)
		require.True(t, ok, "Expected BinaryStep type")

		ctx := &types.ScaffoldContext{
			WorktreePath: "/tmp/myapp/feature-auth",
			SiteName:     "myapp",
			Path:         "feature-auth",
		}

		replacedArgs := binaryStep.replaceTemplate(binaryStep.args, ctx)
		assert.Contains(t, replacedArgs, "--domain=feature-auth.test")
	})

	t.Run("replaces .DbSuffix in args", func(t *testing.T) {
		step, err := Create("php.laravel", config.StepConfig{
			Args: []string{"db:seed", "--database=myapp_{{ .DbSuffix }}"},
		})
		require.NoError(t, err)
		binaryStep, ok := step.(*BinaryStep)
		require.True(t, ok, "Expected BinaryStep type")

		ctx := &types.ScaffoldContext{
			WorktreePath: "/tmp",
		}
		ctx.SetDbSuffix("swift_runner")

		replacedArgs := binaryStep.replaceTemplate(binaryStep.args, ctx)
		assert.Contains(t, replacedArgs, "--database=myapp_swift_runner")
	})

	t.Run("replaces dynamic variables from context", func(t *testing.T) {
		step, err := Create("php.composer", config.StepConfig{
			Args: []string{"install", "--name={{ .VarName }}"},
		})
		require.NoError(t, err)
		binaryStep, ok := step.(*BinaryStep)
		require.True(t, ok, "Expected BinaryStep type")

		ctx := &types.ScaffoldContext{
			WorktreePath: "/tmp",
		}
		ctx.SetVar("VarName", "custom_value")

		replacedArgs := binaryStep.replaceTemplate(binaryStep.args, ctx)
		assert.Contains(t, replacedArgs, "--name=custom_value")
	})

	t.Run("handles whitespace variations in template syntax", func(t *testing.T) {
		step, err := Create("php.composer", config.StepConfig{
			Args: []string{"--name={{ .SiteName }}", "--repo={{.RepoName}}", "--path={{  .Path  }}"},
		})
		require.NoError(t, err)
		binaryStep, ok := step.(*BinaryStep)
		require.True(t, ok, "Expected BinaryStep type")

		ctx := &types.ScaffoldContext{
			WorktreePath: "/tmp",
			SiteName:     "myapp",
			RepoName:     "myrepo",
			Path:         "mypath",
		}

		replacedArgs := binaryStep.replaceTemplate(binaryStep.args, ctx)
		assert.Contains(t, replacedArgs, "--name=myapp")
		assert.Contains(t, replacedArgs, "--repo=myrepo")
		assert.Contains(t, replacedArgs, "--path=mypath")
	})

	t.Run("ignores template errors for invalid syntax", func(t *testing.T) {
		step, err := Create("php.composer", config.StepConfig{
			Args: []string{"--name={{ invalid_syntax }", "--fallback=value"},
		})
		require.NoError(t, err)
		binaryStep, ok := step.(*BinaryStep)
		require.True(t, ok, "Expected BinaryStep type")

		ctx := &types.ScaffoldContext{
			WorktreePath: "/tmp",
			SiteName:     "myapp",
		}

		replacedArgs := binaryStep.replaceTemplate(binaryStep.args, ctx)
		assert.Equal(t, "--name={{ invalid_syntax }", replacedArgs[0])
		assert.Equal(t, "--fallback=value", replacedArgs[1])
	})
}

func TestConditionEvaluator_fileHasScript(t *testing.T) {
	tmpDir := t.TempDir()

	ctx := &types.ScaffoldContext{
		WorktreePath: tmpDir,
		Branch:       "test",
	}

	t.Run("file_has_script - script exists in package.json", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(`{"scripts": {"build": "echo build", "test": "echo test"}}`), 0644))

		result, err := ctx.EvaluateCondition(map[string]interface{}{
			"file_has_script": "build",
		})
		assert.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("file_has_script - script does not exist", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(`{"scripts": {"build": "echo build"}}`), 0644))

		result, err := ctx.EvaluateCondition(map[string]interface{}{
			"file_has_script": "deploy",
		})
		assert.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("file_has_script - package.json does not exist", func(t *testing.T) {
		require.NoError(t, os.Remove(filepath.Join(tmpDir, "package.json")))

		result, err := ctx.EvaluateCondition(map[string]interface{}{
			"file_has_script": "build",
		})
		assert.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("file_has_script - empty script name", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(`{"scripts": {}}`), 0644))

		result, err := ctx.EvaluateCondition(map[string]interface{}{
			"file_has_script": "",
		})
		assert.NoError(t, err)
		assert.False(t, result)
	})
}

func TestBinaryStep_OutputCapture(t *testing.T) {
	t.Run("stores output in context when store_as is set", func(t *testing.T) {
		step := NewBinaryStep("test.echo", "echo", []string{"hello world"}, "Greeting")
		ctx := &types.ScaffoldContext{WorktreePath: t.TempDir()}

		err := step.Run(ctx, types.StepOptions{})

		assert.NoError(t, err)
		assert.Equal(t, "hello world", ctx.GetVar("Greeting"))
	})

	t.Run("trims whitespace from captured output", func(t *testing.T) {
		step := NewBinaryStep("test.echo", "echo", []string{"  spaced  "}, "Spaced")
		ctx := &types.ScaffoldContext{WorktreePath: t.TempDir()}

		err := step.Run(ctx, types.StepOptions{})

		assert.NoError(t, err)
		assert.Equal(t, "spaced", ctx.GetVar("Spaced"))
	})

	t.Run("does not store output when store_as is empty", func(t *testing.T) {
		step := NewBinaryStep("test.echo", "echo", []string{"hello"}, "")
		ctx := &types.ScaffoldContext{WorktreePath: t.TempDir()}

		err := step.Run(ctx, types.StepOptions{})

		assert.NoError(t, err)
		assert.Equal(t, "", ctx.GetVar("AnyVar"))
	})

	t.Run("does not store output on command failure", func(t *testing.T) {
		step := NewBinaryStep("test.false", "false", []string{}, "ErrorMsg")
		ctx := &types.ScaffoldContext{WorktreePath: t.TempDir()}

		err := step.Run(ctx, types.StepOptions{})

		assert.Error(t, err)
		assert.Equal(t, "", ctx.GetVar("ErrorMsg"))
	})

	t.Run("captures multiline output", func(t *testing.T) {
		step := NewBinaryStep("test.printf", "printf", []string{"line1\\nline2\\nline3"}, "Lines")
		ctx := &types.ScaffoldContext{WorktreePath: t.TempDir()}

		err := step.Run(ctx, types.StepOptions{})

		assert.NoError(t, err)
		assert.Equal(t, "line1\nline2\nline3", ctx.GetVar("Lines"))
	})

	t.Run("overwrites existing variable", func(t *testing.T) {
		step := NewBinaryStep("test.echo", "echo", []string{"new value"}, "MyVar")
		ctx := &types.ScaffoldContext{WorktreePath: t.TempDir()}
		ctx.SetVar("MyVar", "old value")

		err := step.Run(ctx, types.StepOptions{})

		assert.NoError(t, err)
		assert.Equal(t, "new value", ctx.GetVar("MyVar"))
	})

	t.Run("creates step via Create with store_as", func(t *testing.T) {
		step, err := Create("php", config.StepConfig{
			Args:    []string{"-r", "echo 'hello';"},
			StoreAs: "PhpOutput",
		})
		require.NoError(t, err)

		binaryStep, ok := step.(*BinaryStep)
		require.True(t, ok, "Expected BinaryStep type")
		assert.Equal(t, "PhpOutput", binaryStep.storeAs)
	})
}
