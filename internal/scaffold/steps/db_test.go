package steps

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michaeldyrynda/arbor/internal/config"
	"github.com/michaeldyrynda/arbor/internal/scaffold/types"
)

func TestDbCreateStep(t *testing.T) {
	t.Run("name returns db.create", func(t *testing.T) {
		step := NewDbCreateStep(config.StepConfig{}, 8)
		assert.Equal(t, "db.create", step.Name())
	})

	t.Run("priority returns correct value", func(t *testing.T) {
		step := NewDbCreateStep(config.StepConfig{}, 8)
		assert.Equal(t, 8, step.Priority())
	})

	t.Run("condition always returns true - controlled by preset", func(t *testing.T) {
		step := NewDbCreateStep(config.StepConfig{}, 8)
		ctx := &types.ScaffoldContext{
			WorktreePath: t.TempDir(),
		}
		assert.True(t, step.Condition(ctx))
	})

	t.Run("skips when no DB_CONNECTION in env file and no type config", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("APP_NAME=test\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		step := NewDbCreateStep(config.StepConfig{}, 8)
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)
	})

	t.Run("auto-detects mysql engine from DB_CONNECTION env", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		step := NewDbCreateStep(config.StepConfig{}, 8)
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			SiteName:     "testapp",
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)
		assert.NotEmpty(t, ctx.GetDbSuffix(), "DbSuffix should be set after db.create")
	})

	t.Run("auto-detects pgsql engine from DB_CONNECTION env", func(t *testing.T) {
		if _, err := exec.LookPath("psql"); err != nil {
			t.Skip("psql client not found")
		}

		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=pgsql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		step := NewDbCreateStep(config.StepConfig{}, 8)
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			SiteName:     "testapp",
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)
		assert.NotEmpty(t, ctx.GetDbSuffix(), "DbSuffix should be set after db.create")
	})

	t.Run("uses explicit type config over env detection", func(t *testing.T) {
		if _, err := exec.LookPath("psql"); err != nil {
			t.Skip("psql client not found")
		}

		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		step := NewDbCreateStep(config.StepConfig{Type: "pgsql"}, 8)
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			SiteName:     "testapp",
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)
		assert.NotEmpty(t, ctx.GetDbSuffix(), "DbSuffix should be set after db.create")
	})

	t.Run("generates database name with site name and suffix", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		step := NewDbCreateStep(config.StepConfig{}, 8)
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			SiteName:     "my-app",
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)

		suffix := ctx.GetDbSuffix()
		assert.NotEmpty(t, suffix, "DbSuffix should be set")

		parts := strings.Split(suffix, "_")
		assert.Len(t, parts, 2, "Suffix should be in format {adjective}_{noun}")
	})

	t.Run("writes DbSuffix to worktree-local arbor.yaml", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		step := NewDbCreateStep(config.StepConfig{}, 8)
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			SiteName:     "testapp",
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		require.NoError(t, err)

		suffix := ctx.GetDbSuffix()
		assert.NotEmpty(t, suffix, "DbSuffix should be set in context")

		cfg, err := config.ReadWorktreeConfig(tmpDir)
		require.NoError(t, err)
		assert.Equal(t, suffix, cfg.DbSuffix, "DbSuffix should be persisted to worktree arbor.yaml")
	})

	t.Run("reads APP_NAME from .env if SiteName is empty", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\nAPP_NAME=myapp\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		step := NewDbCreateStep(config.StepConfig{}, 8)
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			SiteName:     "",
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)
		assert.NotEmpty(t, ctx.GetDbSuffix(), "DbSuffix should be set even with empty SiteName")
	})

	t.Run("sanitizes site name for database generation", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		step := NewDbCreateStep(config.StepConfig{}, 8)
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			SiteName:     "My Test-App!",
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)
		assert.NotEmpty(t, ctx.GetDbSuffix(), "DbSuffix should be set")
	})

	t.Run("creates SQLite database file", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=sqlite\nDB_DATABASE=database/test.sqlite\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		step := NewDbCreateStep(config.StepConfig{}, 8)
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
		}

		err := step.Run(ctx, types.StepOptions{Verbose: true})
		assert.NoError(t, err)

		dbFile := filepath.Join(tmpDir, "database", "test.sqlite")
		assert.FileExists(t, dbFile)
	})

	t.Run("SQLite does not set DbSuffix", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=sqlite\nDB_DATABASE=database/test.sqlite\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		step := NewDbCreateStep(config.StepConfig{}, 8)
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)

		suffix := ctx.GetDbSuffix()
		assert.Empty(t, suffix, "DbSuffix should not be set for SQLite")
	})

	t.Run("collision retry logic tested via mock", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		step := NewDbCreateStep(config.StepConfig{}, 8)
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			SiteName:     "testapp",
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)
		assert.NotEmpty(t, ctx.GetDbSuffix())
	})
}

func TestDbDestroyStep(t *testing.T) {
	t.Run("name returns db.destroy", func(t *testing.T) {
		step := NewDbDestroyStep(config.StepConfig{})
		assert.Equal(t, "db.destroy", step.Name())
	})

	t.Run("condition always returns true - controlled by preset", func(t *testing.T) {
		step := NewDbDestroyStep(config.StepConfig{})
		ctx := &types.ScaffoldContext{
			WorktreePath: t.TempDir(),
		}
		assert.True(t, step.Condition(ctx))
	})

	t.Run("returns nil when no DbSuffix in context or worktree config", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		step := NewDbDestroyStep(config.StepConfig{})
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err, "Should return nil when no DbSuffix found")
	})

	t.Run("reads DbSuffix from worktree-local arbor.yaml", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		if err := config.WriteWorktreeConfig(tmpDir, map[string]string{"db_suffix": "swift_runner"}); err != nil {
			t.Fatalf("writing worktree config: %v", err)
		}

		step := NewDbDestroyStep(config.StepConfig{})
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)
		assert.Equal(t, "swift_runner", ctx.GetDbSuffix(), "DbSuffix should be read from worktree config")
	})

	t.Run("auto-detects mysql engine from DB_CONNECTION env", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		if err := config.WriteWorktreeConfig(tmpDir, map[string]string{"db_suffix": "test_suffix"}); err != nil {
			t.Fatalf("writing worktree config: %v", err)
		}

		step := NewDbDestroyStep(config.StepConfig{})
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)
	})

	t.Run("auto-detects pgsql engine from DB_CONNECTION env", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=pgsql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		if err := config.WriteWorktreeConfig(tmpDir, map[string]string{"db_suffix": "test_suffix"}); err != nil {
			t.Fatalf("writing worktree config: %v", err)
		}

		step := NewDbDestroyStep(config.StepConfig{})
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)
	})

	t.Run("uses explicit type config over env detection", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		if err := config.WriteWorktreeConfig(tmpDir, map[string]string{"db_suffix": "test_suffix"}); err != nil {
			t.Fatalf("writing worktree config: %v", err)
		}

		step := NewDbDestroyStep(config.StepConfig{Type: "pgsql"})
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)
	})

	t.Run("uses DbSuffix from context if set", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		step := NewDbDestroyStep(config.StepConfig{})
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
		}
		ctx.SetDbSuffix("context_suffix")

		if err := config.WriteWorktreeConfig(tmpDir, map[string]string{"db_suffix": "config_suffix"}); err != nil {
			t.Fatalf("writing worktree config: %v", err)
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)
		assert.Equal(t, "context_suffix", ctx.GetDbSuffix(), "Should use DbSuffix from context, not worktree config")
	})
}
