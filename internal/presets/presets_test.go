package presets

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	assert.Len(t, steps, 12)

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
	assert.Equal(t, []string{"key:generate", "--show", "--no-interaction"}, steps[3].Args)
	assert.Equal(t, "AppKey", steps[3].StoreAs)

	assert.Equal(t, "env.write", steps[4].Name)
	assert.Equal(t, "APP_KEY", steps[4].Key)
	assert.Equal(t, "{{ .AppKey }}", steps[4].Value)

	assert.Equal(t, "db.create", steps[5].Name)

	assert.Equal(t, "env.write", steps[6].Name)
	assert.Equal(t, "DB_DATABASE", steps[6].Key)
	assert.Equal(t, "{{ .SanitizedSiteName }}_{{ .DbSuffix }}", steps[6].Value)

	assert.Equal(t, "node.npm", steps[7].Name)
	assert.Equal(t, []string{"ci"}, steps[7].Args)
	assert.NotNil(t, steps[7].Condition, "npm ci should have a condition")
	assert.Equal(t, "package-lock.json", steps[7].Condition["file_exists"])

	assert.Equal(t, "php.laravel", steps[8].Name)
	assert.Equal(t, []string{"migrate:fresh", "--seed", "--no-interaction"}, steps[8].Args)

	assert.Equal(t, "node.npm", steps[9].Name)
	assert.Equal(t, []string{"run", "build"}, steps[9].Args)
	assert.NotNil(t, steps[9].Condition, "npm run build should have a condition")
	assert.Equal(t, "package-lock.json", steps[9].Condition["file_exists"])
}

func TestLaravelPreset_CleanupSteps(t *testing.T) {
	preset := NewLaravel()
	steps := preset.CleanupSteps()

	assert.Len(t, steps, 2)
	assert.Equal(t, "herd", steps[0].Name)
	assert.Equal(t, "db.destroy", steps[1].Name)
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
