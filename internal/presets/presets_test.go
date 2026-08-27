package presets

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	anvil_exec "github.com/naoray/anvil/internal/exec"
	"github.com/naoray/anvil/internal/scaffold"
	scaffoldsteps "github.com/naoray/anvil/internal/scaffold/steps"
	"github.com/naoray/anvil/internal/scaffold/types"
)

func TestLaravelPreset_Detect(t *testing.T) {
	t.Run("detects with both composer.json and artisan", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "composer.json"), []byte(`{"name": "test/app"}`), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "artisan"), []byte("#!/usr/bin/env php"), 0644))

		preset := NewLaravel()
		assert.True(t, preset.Detect(tmpDir))
	})

	t.Run("does not detect with only artisan", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "artisan"), []byte("#!/usr/bin/env php"), 0644))

		preset := NewLaravel()
		assert.False(t, preset.Detect(tmpDir))
	})

	t.Run("does not detect with only composer.json", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "composer.json"), []byte(`{"name": "test/app"}`), 0644))

		preset := NewLaravel()
		assert.False(t, preset.Detect(tmpDir))
	})

	t.Run("does not detect laravel package with framework dependency", func(t *testing.T) {
		tmpDir := t.TempDir()
		composerJSON := `{"name": "vendor/laravel-package", "require": {"laravel/framework": "^10.0"}}`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "composer.json"), []byte(composerJSON), 0644))

		preset := NewLaravel()
		assert.False(t, preset.Detect(tmpDir))
	})

	t.Run("does not detect empty directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		preset := NewLaravel()
		assert.False(t, preset.Detect(tmpDir))
	})
}

func TestLaravelPreset_Name(t *testing.T) {
	preset := NewLaravel()
	assert.Equal(t, "laravel", preset.Name())
}

func TestLaravelPreset_DefaultSteps(t *testing.T) {
	preset := NewLaravel()
	steps := preset.DefaultSteps()

	require.Len(t, steps, 15)

	assert.Equal(t, "php.composer", steps[0].Name)
	assert.Equal(t, []string{"install"}, steps[0].Args)
	assert.Equal(t, "composer.lock", steps[0].Condition["file_exists"])

	assert.Equal(t, "php.composer", steps[1].Name)
	assert.Equal(t, []string{"update"}, steps[1].Args)
	assert.NotNil(t, steps[1].Condition["not"])

	assert.Equal(t, "file.copy", steps[2].Name)
	assert.Equal(t, ".env.example", steps[2].From)
	assert.Equal(t, ".env", steps[2].To)

	assert.Equal(t, "php.laravel", steps[3].Name)
	assert.Equal(t, []string{"key:generate", "--show", "--no-interaction", "--no-ansi"}, steps[3].Args)
	assert.Equal(t, "AppKey", steps[3].StoreAs)

	assert.Equal(t, "env.write", steps[4].Name)
	assert.Equal(t, "APP_KEY", steps[4].Key)
	assert.Equal(t, "{{ .AppKey }}", steps[4].Value)

	assert.Equal(t, "db.create", steps[5].Name)

	assert.Equal(t, "env.write", steps[6].Name)
	assert.Equal(t, "DB_DATABASE", steps[6].Key)
	assert.Equal(t, "{{ .DatabaseName }}", steps[6].Value)

	assert.Equal(t, "php", steps[7].Name)
	assert.Equal(t, []string{"vendor/bin/phpstan", "clear-result-cache"}, steps[7].Args)
	assert.Equal(t, "vendor/bin/phpstan", steps[7].Condition["file_exists"])

	assert.Equal(t, "db.create", steps[8].Name)
	assert.Equal(t, config.DbRoleTesting, steps[8].Role)
	assert.NotNil(t, steps[8].Condition)

	assert.Equal(t, "node.npm", steps[9].Name)
	assert.Equal(t, []string{"ci"}, steps[9].Args)
	assert.NotNil(t, steps[9].Condition, "npm ci should have a condition")
	assert.Equal(t, "package-lock.json", steps[9].Condition["file_exists"])

	assert.Equal(t, "php.laravel", steps[10].Name)
	assert.Equal(t, []string{"migrate:fresh", "--seed", "--no-interaction"}, steps[10].Args)

	assert.Equal(t, "node.npm", steps[11].Name)
	assert.Equal(t, []string{"run", "build"}, steps[11].Args)
	assert.NotNil(t, steps[11].Condition, "npm run build should have a condition")
	assert.Equal(t, "package-lock.json", steps[11].Condition["file_exists"])

	assert.Equal(t, "php.laravel", steps[12].Name)
	assert.Equal(t, []string{"storage:link", "--no-interaction"}, steps[12].Args)

	assert.Equal(t, "yerd", steps[13].Name)
	assert.Equal(t, []string{"link", "{{ .SiteName }}"}, steps[13].Args)
	assert.Equal(t, "yerd", steps[14].Name)
	assert.Equal(t, []string{"secure", "{{ .SiteName }}"}, steps[14].Args)
}

func TestLaravelPreset_HerdCompatibility(t *testing.T) {
	steps := NewLaravel(config.SiteDriverHerd).DefaultSteps()

	require.Len(t, steps, 14)
	assert.Equal(t, "herd", steps[13].Name)
	assert.Equal(t, []string{"link", "--secure", "{{ .SiteName }}"}, steps[13].Args)
}

func TestLaravelPreset_PHPStanCacheStepIsNoOpWithoutBinary(t *testing.T) {
	tmpDir := t.TempDir()
	cacheStepConfig := laravelPHPStanCacheStepConfig(t)
	registry := scaffoldsteps.NewRegistry()
	registry.RegisterDefaults()
	cacheStep, err := registry.Create(cacheStepConfig.Name, cacheStepConfig)
	require.NoError(t, err)

	ctx := &types.ScaffoldContext{WorktreePath: tmpDir}
	executor := scaffold.NewStepExecutor([]types.ScaffoldStep{cacheStep}, ctx, types.StepOptions{Quiet: true})

	require.NoError(t, executor.Execute())
	require.Len(t, executor.Results(), 1)
	assert.True(t, executor.Results()[0].Skipped, "PHPStan cache invalidation should be skipped when vendor/bin/phpstan is absent")
}

func TestLaravelPreset_DelayedScaffoldClearsPHPStanCacheAfterEnvWrites(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".env.example"), []byte("APP_KEY=\nDB_CONNECTION=sqlite\nDB_DATABASE=\n"), 0644))

	phpstanPath := filepath.Join(tmpDir, "vendor", "bin", "phpstan")
	require.NoError(t, os.MkdirAll(filepath.Dir(phpstanPath), 0755))
	require.NoError(t, os.WriteFile(phpstanPath, nil, 0644))

	ctx := &types.ScaffoldContext{WorktreePath: tmpDir}
	ctx.SetVar("AppKey", "generated-key")
	ctx.SetVar("DatabaseName", "feature_test")

	commander := &phpstanInvocationCommander{t: t, worktreePath: tmpDir}
	commandExecutor := anvil_exec.NewCommandExecutor(commander)
	registry := scaffoldsteps.NewRegistry()
	registry.RegisterDefaults()

	var relevantSteps []types.ScaffoldStep
	for _, stepConfig := range NewLaravel().DefaultSteps() {
		if stepConfig.Name == "php" {
			relevantSteps = append(relevantSteps, scaffoldsteps.NewBinaryStepWithConditionAndExecutor(stepConfig.Name, stepConfig, "php", commandExecutor))
			continue
		}

		if stepConfig.Name != config.StepFileCopy && stepConfig.Name != config.StepEnvWrite {
			continue
		}

		step, err := registry.Create(stepConfig.Name, stepConfig)
		require.NoError(t, err)
		relevantSteps = append(relevantSteps, step)
	}

	_, err := os.Stat(filepath.Join(tmpDir, ".env"))
	require.ErrorIs(t, err, os.ErrNotExist, "the delayed scaffold should start before .env exists")

	executor := scaffold.NewStepExecutor(relevantSteps, ctx, types.StepOptions{Quiet: true})
	require.NoError(t, executor.Execute())
	require.Len(t, executor.Results(), 4)
	assert.Equal(t, config.StepEnvWrite, executor.Results()[2].Step.Name())
	assert.Equal(t, "php", executor.Results()[3].Step.Name())

	envContent, err := os.ReadFile(filepath.Join(tmpDir, ".env"))
	require.NoError(t, err)
	assert.Contains(t, string(envContent), "APP_KEY=generated-key")
	assert.Contains(t, string(envContent), "DB_DATABASE=feature_test")

	require.NotNil(t, commander.call, "PHPStan cache invalidation should reach the command boundary")
	assert.Equal(t, tmpDir, commander.call.Dir)
	assert.Equal(t, "php", commander.call.Command)
	assert.Equal(t, []string{"vendor/bin/phpstan", "clear-result-cache"}, commander.call.Args)
}

type phpstanInvocationCommander struct {
	t            *testing.T
	worktreePath string
	call         *commandCall
}

type commandCall struct {
	Dir     string
	Command string
	Args    []string
}

func (c *phpstanInvocationCommander) Run(_ context.Context, dir string, command string, args ...string) ([]byte, error) {
	c.t.Helper()

	require.Equal(c.t, c.worktreePath, dir)
	envContent, err := os.ReadFile(filepath.Join(dir, ".env"))
	require.NoError(c.t, err)
	require.Contains(c.t, string(envContent), "APP_KEY=generated-key")
	require.Contains(c.t, string(envContent), "DB_DATABASE=feature_test")

	c.call = &commandCall{
		Dir:     dir,
		Command: command,
		Args:    append([]string(nil), args...),
	}

	return nil, nil
}

func laravelPHPStanCacheStepConfig(t *testing.T) config.StepConfig {
	t.Helper()

	for _, step := range NewLaravel().DefaultSteps() {
		if step.Name == "php" && assert.ObjectsAreEqual([]string{"vendor/bin/phpstan", "clear-result-cache"}, step.Args) {
			return step
		}
	}

	t.Fatal("Laravel preset should include a PHPStan result-cache invalidation step")
	return config.StepConfig{}
}

func TestLaravelPreset_CleanupSteps(t *testing.T) {
	preset := NewLaravel()
	steps := preset.CleanupSteps()

	assert.Len(t, steps, 2)
	assert.Equal(t, "yerd", steps[0].Name)
	assert.Equal(t, []string{"unlink", "{{ .SiteName }}"}, steps[0].Args)
	assert.Equal(t, "db.destroy", steps[1].Name)
}

func TestLaravelSharedDBPreset_HasNoDatabaseSteps(t *testing.T) {
	preset := NewLaravelSharedDB()
	for _, step := range preset.DefaultSteps() {
		assert.NotEqual(t, config.StepDbCreate, step.Name)
		assert.NotEqual(t, config.StepDbDestroy, step.Name)
	}
	for _, step := range preset.CleanupSteps() {
		assert.NotEqual(t, config.StepDbCreate, step.Name)
		assert.NotEqual(t, config.StepDbDestroy, step.Name)
	}
}

func TestLaravelSharedDBPreset_DefaultStepsAreConstructible(t *testing.T) {
	registry := scaffoldsteps.NewRegistry()
	registry.RegisterDefaults()
	manager := scaffold.NewScaffoldManagerWithRegistry(registry)
	resolved := NewManager().Resolve("laravel-shared-db", "", "")

	steps, err := manager.GetStepsForWorktree(
		&config.Config{Preset: "laravel-shared-db"},
		t.TempDir(),
		"feature/yerd",
		resolved.Name(),
		resolved.DefaultSteps(),
	)
	require.NoError(t, err)
	require.Len(t, steps, 9)
	assert.Equal(t, "yerd", steps[7].Name())
	assert.Equal(t, "yerd", steps[8].Name())
}

func TestPHPPreset_Detect(t *testing.T) {
	t.Run("detects by composer.json", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := os.WriteFile(filepath.Join(tmpDir, "composer.json"), []byte(`{"name": "test/app"}`), 0644)
		require.NoError(t, err)

		preset := NewPHP()
		assert.True(t, preset.Detect(tmpDir))
	})

	t.Run("does not detect without composer.json", func(t *testing.T) {
		tmpDir := t.TempDir()

		preset := NewPHP()
		assert.False(t, preset.Detect(tmpDir))
	})
}

func TestPHPPreset_Name(t *testing.T) {
	preset := NewPHP()
	assert.Equal(t, "php", preset.Name())
}

func TestPHPPreset_DefaultSteps(t *testing.T) {
	preset := NewPHP()
	steps := preset.DefaultSteps()

	assert.Len(t, steps, 2)

	assert.Equal(t, "php.composer", steps[0].Name)
	assert.Equal(t, []string{"install"}, steps[0].Args)
	assert.Equal(t, "composer.lock", steps[0].Condition["file_exists"])

	assert.Equal(t, "php.composer", steps[1].Name)
	assert.Equal(t, []string{"update"}, steps[1].Args)
	assert.NotNil(t, steps[1].Condition["not"])
}

func TestPHPPreset_CleanupSteps(t *testing.T) {
	preset := NewPHP()
	steps := preset.CleanupSteps()

	assert.Nil(t, steps)
}

func TestManager_RegisterAndGet(t *testing.T) {
	m := NewManager()

	laravel, ok := m.Get("laravel")
	assert.True(t, ok)
	assert.Equal(t, "laravel", laravel.Name())

	php, ok := m.Get("php")
	assert.True(t, ok)
	assert.Equal(t, "php", php.Name())

	_, ok = m.Get("nonexistent")
	assert.False(t, ok)
}

func TestManager_Detect(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "composer.json"), []byte(`{"name": "test/app"}`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "artisan"), []byte("#!/usr/bin/env php"), 0644))

	m := NewManager()
	detected := m.Detect(tmpDir)
	assert.Equal(t, "laravel", detected)
}

func TestManager_Suggest(t *testing.T) {
	t.Run("returns detected preset", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "composer.json"), []byte(`{"name": "test/app"}`), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "artisan"), []byte("#!/usr/bin/env php"), 0644))

		m := NewManager()
		suggested := m.Suggest(tmpDir)
		assert.Equal(t, "laravel", suggested)
	})

	t.Run("returns php for unknown project", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Test"), 0644)
		require.NoError(t, err)

		m := NewManager()
		suggested := m.Suggest(tmpDir)
		assert.Equal(t, "php", suggested)
	})
}

func TestManager_Available(t *testing.T) {
	m := NewManager()
	available := m.Available()

	assert.Len(t, available, 3)
	assert.Contains(t, available, "laravel")
	assert.Contains(t, available, "php")
	assert.Contains(t, available, "laravel-shared-db")
}

func TestManager_RegisterRejectsDuplicateNames(t *testing.T) {
	m := NewManager()

	assert.Panics(t, func() {
		m.Register(NewPHP())
	})
}

func TestManager_AvailablePreservesDetectionOrder(t *testing.T) {
	assert.Equal(t, []string{"laravel-shared-db", "laravel", "php"}, NewManager().Available())
}

func TestManager_ResolveConfiguredPresetBeforeDetection(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "composer.json"), []byte(`{"name": "test/app"}`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "artisan"), []byte("#!/usr/bin/env php"), 0644))

	resolved := NewManager().Resolve("", "php", tmpDir)

	assert.Equal(t, "php", resolved.Name())
}

func TestManager_ResolvePreservesUnknownConfiguredName(t *testing.T) {
	resolved := NewManager().Resolve("", "unknown", t.TempDir())

	assert.Equal(t, "unknown", resolved.Name())
	assert.Nil(t, resolved.DefaultSteps())
	assert.Nil(t, resolved.CleanupSteps())
}

func TestManager_ResolveDoesNotAutoDetectLaravelSharedDB(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "composer.json"), []byte(`{"name": "test/app"}`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "artisan"), []byte("#!/usr/bin/env php"), 0644))

	resolved := NewManager().Resolve("", "", tmpDir)

	assert.Equal(t, "laravel", resolved.Name())
}

func TestResolvedPreset_ReturnsIndependentStepCopies(t *testing.T) {
	manager := NewManager()
	resolved := manager.Resolve("laravel", "", "")

	defaultSteps := resolved.DefaultSteps()
	defaultSteps[0].Args[0] = "changed"
	defaultSteps[0].Condition["file_exists"] = "changed"
	cleanupSteps := resolved.CleanupSteps()
	cleanupSteps[0].Args[0] = "changed"

	fresh := manager.Resolve("laravel", "", "")
	assert.Equal(t, []string{"install"}, fresh.DefaultSteps()[0].Args)
	assert.Equal(t, "composer.lock", fresh.DefaultSteps()[0].Condition["file_exists"])
	assert.Equal(t, []string{"unlink", "{{ .SiteName }}"}, fresh.CleanupSteps()[0].Args)
}

func TestManager_ResolveExplicitPresetOverridesConfiguredAndDetection(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "composer.json"), []byte(`{"name": "test/app"}`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "artisan"), []byte("#!/usr/bin/env php"), 0644))

	resolved := NewManager().Resolve("php", "laravel", tmpDir)

	assert.Equal(t, "php", resolved.Name())
	assert.Len(t, resolved.DefaultSteps(), 2)
	assert.Nil(t, resolved.CleanupSteps())
}
