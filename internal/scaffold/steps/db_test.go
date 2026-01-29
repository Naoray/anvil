package steps

import (
	"errors"
	"os"
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
		step := NewDbCreateStep(config.StepConfig{})
		assert.Equal(t, "db.create", step.Name())
	})

	t.Run("condition always returns true - controlled by preset", func(t *testing.T) {
		step := NewDbCreateStep(config.StepConfig{})
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

		step := NewDbCreateStep(config.StepConfig{})
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)
	})

	t.Run("creates database with mock client", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		mockClient := NewMockDatabaseClient()
		step := NewDbCreateStepWithFactory(config.StepConfig{}, MockClientFactory(mockClient))
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			SiteName:     "testapp",
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)
		assert.NotEmpty(t, ctx.GetDbSuffix(), "DbSuffix should be set after db.create")
		assert.Equal(t, 1, mockClient.DatabaseCount(), "Should have created one database")
	})

	t.Run("auto-detects mysql engine from DB_CONNECTION env", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		mockClient := NewMockDatabaseClient()
		step := NewDbCreateStepWithFactory(config.StepConfig{}, MockClientFactory(mockClient))
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			SiteName:     "testapp",
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)
		assert.NotEmpty(t, ctx.GetDbSuffix(), "DbSuffix should be set after db.create")
	})

	t.Run("auto-detects pgsql engine from DB_CONNECTION env", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=pgsql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		mockClient := NewMockDatabaseClient()
		step := NewDbCreateStepWithFactory(config.StepConfig{}, MockClientFactory(mockClient))
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			SiteName:     "testapp",
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)
		assert.NotEmpty(t, ctx.GetDbSuffix(), "DbSuffix should be set after db.create")
	})

	t.Run("uses explicit type config over env detection", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		mockClient := NewMockDatabaseClient()
		step := NewDbCreateStepWithFactory(config.StepConfig{Type: "pgsql"}, MockClientFactory(mockClient))
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

		mockClient := NewMockDatabaseClient()
		step := NewDbCreateStepWithFactory(config.StepConfig{}, MockClientFactory(mockClient))
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

		createCalls := mockClient.GetCreateCalls()
		assert.Len(t, createCalls, 1, "Should have one create call")
		assert.True(t, strings.HasPrefix(createCalls[0], "my_app_"), "Database name should start with sanitized site name")
	})

	t.Run("writes DbSuffix to worktree-local arbor.yaml", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		mockClient := NewMockDatabaseClient()
		step := NewDbCreateStepWithFactory(config.StepConfig{}, MockClientFactory(mockClient))
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

		mockClient := NewMockDatabaseClient()
		step := NewDbCreateStepWithFactory(config.StepConfig{}, MockClientFactory(mockClient))
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			SiteName:     "",
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)
		assert.NotEmpty(t, ctx.GetDbSuffix(), "DbSuffix should be set even with empty SiteName")

		createCalls := mockClient.GetCreateCalls()
		assert.Len(t, createCalls, 1)
		assert.True(t, strings.HasPrefix(createCalls[0], "myapp_"), "Should use APP_NAME from .env")
	})

	t.Run("sanitizes site name for database generation", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		mockClient := NewMockDatabaseClient()
		step := NewDbCreateStepWithFactory(config.StepConfig{}, MockClientFactory(mockClient))
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			SiteName:     "My Test-App!",
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)
		assert.NotEmpty(t, ctx.GetDbSuffix(), "DbSuffix should be set")

		createCalls := mockClient.GetCreateCalls()
		assert.Len(t, createCalls, 1)
		assert.True(t, strings.HasPrefix(createCalls[0], "my_test_app_"), "Site name should be sanitized")
	})

	t.Run("creates SQLite database file", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=sqlite\nDB_DATABASE=database/test.sqlite\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		step := NewDbCreateStep(config.StepConfig{})
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

		step := NewDbCreateStep(config.StepConfig{})
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)
		assert.Empty(t, ctx.GetDbSuffix(), "DbSuffix should not be set for SQLite")
	})

	t.Run("creates database with custom prefix", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		mockClient := NewMockDatabaseClient()
		step := NewDbCreateStepWithFactory(config.StepConfig{
			Args: []string{"--prefix", "mycustom"},
		}, MockClientFactory(mockClient))
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			SiteName:     "testapp",
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)

		suffix := ctx.GetDbSuffix()
		assert.NotEmpty(t, suffix, "DbSuffix should be set")

		createCalls := mockClient.GetCreateCalls()
		assert.Len(t, createCalls, 1)
		assert.True(t, strings.HasPrefix(createCalls[0], "mycustom_"), "Should use custom prefix")

		cfg, err := config.ReadWorktreeConfig(tmpDir)
		require.NoError(t, err)
		assert.Equal(t, suffix, cfg.DbSuffix, "Suffix should be persisted to worktree config")
	})

	t.Run("creates database without prefix uses siteName", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		mockClient := NewMockDatabaseClient()
		step := NewDbCreateStepWithFactory(config.StepConfig{}, MockClientFactory(mockClient))
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			SiteName:     "myapp",
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)
		assert.NotEmpty(t, ctx.GetDbSuffix())

		createCalls := mockClient.GetCreateCalls()
		assert.True(t, strings.HasPrefix(createCalls[0], "myapp_"))
	})

	t.Run("db.create uses existing suffix from context", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		mockClient := NewMockDatabaseClient()
		step := NewDbCreateStepWithFactory(config.StepConfig{}, MockClientFactory(mockClient))
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			SiteName:     "testapp",
		}
		ctx.SetDbSuffix("preexisting_suffix")

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)
		assert.Equal(t, "preexisting_suffix", ctx.GetDbSuffix(), "Should use preexisting suffix from context")

		createCalls := mockClient.GetCreateCalls()
		assert.Len(t, createCalls, 1)
		assert.Equal(t, "testapp_preexisting_suffix", createCalls[0], "Should use preexisting suffix")

		cfg, err := config.ReadWorktreeConfig(tmpDir)
		require.NoError(t, err)
		assert.Equal(t, "preexisting_suffix", cfg.DbSuffix, "Should persist preexisting suffix to worktree config")
	})

	t.Run("db.create with prefix uses existing suffix", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		mockClient := NewMockDatabaseClient()
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			SiteName:     "testapp",
		}
		ctx.SetDbSuffix("shared_suffix")

		step := NewDbCreateStepWithFactory(config.StepConfig{
			Args: []string{"--prefix", "app"},
		}, MockClientFactory(mockClient))

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)
		assert.Equal(t, "shared_suffix", ctx.GetDbSuffix(), "Should use shared suffix from context")

		createCalls := mockClient.GetCreateCalls()
		assert.Len(t, createCalls, 1)
		assert.Equal(t, "app_shared_suffix", createCalls[0], "Should use prefix with shared suffix")
	})

	t.Run("retries on database exists error", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		mockClient := NewMockDatabaseClient()
		mockClient.SetExistsOnFirstNCalls(2)

		step := NewDbCreateStepWithFactory(config.StepConfig{}, MockClientFactory(mockClient))
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			SiteName:     "testapp",
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)

		createCalls := mockClient.GetCreateCalls()
		assert.Len(t, createCalls, 3, "Should have retried 3 times (2 failures + 1 success)")
		assert.Equal(t, 1, mockClient.DatabaseCount(), "Should have created one database")
	})

	t.Run("fails after max retries", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		mockClient := NewMockDatabaseClient()
		mockClient.SetExistsOnFirstNCalls(10)

		step := NewDbCreateStepWithFactory(config.StepConfig{}, MockClientFactory(mockClient))
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			SiteName:     "testapp",
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create database after 5 attempts")
	})

	t.Run("skips when database ping fails", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		mockClient := NewMockDatabaseClient()
		mockClient.SetPingError(errors.New("connection refused"))

		step := NewDbCreateStepWithFactory(config.StepConfig{}, MockClientFactory(mockClient))
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
			SiteName:     "testapp",
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err, "Should not error when ping fails, just skip")
		assert.Empty(t, ctx.GetDbSuffix(), "DbSuffix should not be set when skipped")
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

		mockClient := NewMockDatabaseClient()
		step := NewDbDestroyStepWithFactory(config.StepConfig{}, MockClientFactory(mockClient))
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

		mockClient := NewMockDatabaseClient()
		mockClient.AddDatabase("myapp_swift_runner")

		step := NewDbDestroyStepWithFactory(config.StepConfig{}, MockClientFactory(mockClient))
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
		}

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)
		assert.Equal(t, "swift_runner", ctx.GetDbSuffix(), "DbSuffix should be read from worktree config")

		listCalls := mockClient.listCalls
		assert.Len(t, listCalls, 1)
		assert.Equal(t, "%_swift_runner", listCalls[0])
	})

	t.Run("drops databases matching suffix", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		mockClient := NewMockDatabaseClient()
		mockClient.AddDatabase("app1_test_suffix")
		mockClient.AddDatabase("app2_test_suffix")

		step := NewDbDestroyStepWithFactory(config.StepConfig{}, MockClientFactory(mockClient))
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
		}
		ctx.SetDbSuffix("test_suffix")

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)

		dropCalls := mockClient.GetDropCalls()
		assert.Len(t, dropCalls, 2, "Should have dropped 2 databases")
		assert.Equal(t, 0, mockClient.DatabaseCount(), "All databases should be dropped")
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

		mockClient := NewMockDatabaseClient()
		step := NewDbDestroyStepWithFactory(config.StepConfig{}, MockClientFactory(mockClient))
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

		mockClient := NewMockDatabaseClient()
		step := NewDbDestroyStepWithFactory(config.StepConfig{}, MockClientFactory(mockClient))
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

		mockClient := NewMockDatabaseClient()
		step := NewDbDestroyStepWithFactory(config.StepConfig{Type: "pgsql"}, MockClientFactory(mockClient))
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

		mockClient := NewMockDatabaseClient()
		mockClient.AddDatabase("app_context_suffix")

		step := NewDbDestroyStepWithFactory(config.StepConfig{}, MockClientFactory(mockClient))
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

		listCalls := mockClient.listCalls
		assert.Len(t, listCalls, 1)
		assert.Equal(t, "%_context_suffix", listCalls[0], "Should search with context suffix")
	})

	t.Run("skips when database ping fails", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		mockClient := NewMockDatabaseClient()
		mockClient.SetPingError(errors.New("connection refused"))

		step := NewDbDestroyStepWithFactory(config.StepConfig{}, MockClientFactory(mockClient))
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
		}
		ctx.SetDbSuffix("test_suffix")

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err, "Should not error when ping fails, just skip")
	})

	t.Run("skips sqlite engine", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=sqlite\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		step := NewDbDestroyStep(config.StepConfig{})
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
		}
		ctx.SetDbSuffix("test_suffix")

		err := step.Run(ctx, types.StepOptions{Verbose: false})
		assert.NoError(t, err)
	})

	t.Run("dry run does not drop databases", func(t *testing.T) {
		tmpDir := t.TempDir()

		envFile := filepath.Join(tmpDir, ".env")
		if err := os.WriteFile(envFile, []byte("DB_CONNECTION=mysql\n"), 0644); err != nil {
			t.Fatalf("writing env file: %v", err)
		}

		mockClient := NewMockDatabaseClient()
		mockClient.AddDatabase("app_test_suffix")

		step := NewDbDestroyStepWithFactory(config.StepConfig{}, MockClientFactory(mockClient))
		ctx := &types.ScaffoldContext{
			WorktreePath: tmpDir,
		}
		ctx.SetDbSuffix("test_suffix")

		err := step.Run(ctx, types.StepOptions{Verbose: false, DryRun: true})
		assert.NoError(t, err)

		dropCalls := mockClient.GetDropCalls()
		assert.Len(t, dropCalls, 0, "Should not drop databases in dry run")
		assert.Equal(t, 1, mockClient.DatabaseCount(), "Database should still exist")
	})
}

func TestIsDatabaseExistsError(t *testing.T) {
	t.Run("returns true for DatabaseExistsError", func(t *testing.T) {
		err := &DatabaseExistsError{Name: "test"}
		assert.True(t, IsDatabaseExistsError(err))
	})

	t.Run("returns true for error containing 'already exists'", func(t *testing.T) {
		err := errors.New("database already exists")
		assert.True(t, IsDatabaseExistsError(err))
	})

	t.Run("returns true for error containing '1007'", func(t *testing.T) {
		err := errors.New("Error 1007: Can't create database")
		assert.True(t, IsDatabaseExistsError(err))
	})

	t.Run("returns false for nil error", func(t *testing.T) {
		assert.False(t, IsDatabaseExistsError(nil))
	})

	t.Run("returns false for unrelated error", func(t *testing.T) {
		err := errors.New("connection refused")
		assert.False(t, IsDatabaseExistsError(err))
	})
}
