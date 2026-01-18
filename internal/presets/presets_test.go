package presets

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLaravelPreset_Detect(t *testing.T) {
	t.Run("detects by artisan file", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := ioutil.WriteFile(filepath.Join(tmpDir, "artisan"), []byte("#!/usr/bin/env php"), 0644)
		require.NoError(t, err)

		preset := NewLaravel()
		assert.True(t, preset.Detect(tmpDir))
	})

	t.Run("detects by composer.json with laravel/framework", func(t *testing.T) {
		tmpDir := t.TempDir()
		composerJSON := `{"name": "test/app", "require": {"laravel/framework": "^10.0"}}`
		err := ioutil.WriteFile(filepath.Join(tmpDir, "composer.json"), []byte(composerJSON), 0644)
		require.NoError(t, err)

		preset := NewLaravel()
		assert.True(t, preset.Detect(tmpDir))
	})

	t.Run("does not detect without laravel indicators", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := ioutil.WriteFile(filepath.Join(tmpDir, "composer.json"), []byte(`{"name": "test/app"}`), 0644)
		require.NoError(t, err)

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

	assert.Len(t, steps, 9)

	assert.Equal(t, "php.composer", steps[0].Name)
	assert.Equal(t, []string{"install"}, steps[0].Args)

	assert.Equal(t, "node.npm", steps[1].Name)
	assert.Equal(t, []string{"ci"}, steps[1].Args)

	assert.Equal(t, "database.create", steps[2].Name)

	assert.Equal(t, "php.laravel.artisan", steps[3].Name)
	assert.Equal(t, []string{"key:generate"}, steps[3].Args)

	assert.Equal(t, "file.copy", steps[4].Name)
	assert.Equal(t, ".env.example", steps[4].From)
	assert.Equal(t, ".env", steps[4].To)
}

func TestLaravelPreset_CleanupSteps(t *testing.T) {
	preset := NewLaravel()
	steps := preset.CleanupSteps()

	assert.Len(t, steps, 2)
	assert.Equal(t, "herd", steps[0].Name)
	assert.Equal(t, "bash.run", steps[1].Name)
}

func TestPHPPreset_Detect(t *testing.T) {
	t.Run("detects by composer.json", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := ioutil.WriteFile(filepath.Join(tmpDir, "composer.json"), []byte(`{"name": "test/app"}`), 0644)
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

	assert.Len(t, steps, 1)
	assert.Equal(t, "php.composer", steps[0].Name)
	assert.Equal(t, []string{"install"}, steps[0].Args)
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
	err := ioutil.WriteFile(filepath.Join(tmpDir, "artisan"), []byte("#!/usr/bin/env php"), 0644)
	require.NoError(t, err)

	m := NewManager()
	detected := m.Detect(tmpDir)
	assert.Equal(t, "laravel", detected)
}

func TestManager_Suggest(t *testing.T) {
	t.Run("returns detected preset", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := ioutil.WriteFile(filepath.Join(tmpDir, "artisan"), []byte("#!/usr/bin/env php"), 0644)
		require.NoError(t, err)

		m := NewManager()
		suggested := m.Suggest(tmpDir)
		assert.Equal(t, "laravel", suggested)
	})

	t.Run("returns php for unknown project", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := ioutil.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Test"), 0644)
		require.NoError(t, err)

		m := NewManager()
		suggested := m.Suggest(tmpDir)
		assert.Equal(t, "php", suggested)
	})
}

func TestManager_Available(t *testing.T) {
	m := NewManager()
	available := m.Available()

	assert.Len(t, available, 2)
	assert.Contains(t, available, "laravel")
	assert.Contains(t, available, "php")
}
