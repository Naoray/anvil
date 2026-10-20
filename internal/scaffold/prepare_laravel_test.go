package scaffold

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/presets"
	"github.com/naoray/anvil/internal/scaffold/steps"
)

// TestEnsureLaravelPresetKeepsDatabaseStepsAuthoritative proves idempotent
// preparation never bypasses database ownership: db.create and db.migrate run
// on every ensure, while file/env steps are recorded once.
func TestEnsureLaravelPresetKeepsDatabaseStepsAuthoritative(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell stand-in for php")
	}
	dir := prepareFixture(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env.example"), []byte("APP_KEY=base64:fixture\nDB_CONNECTION=mysql\nDB_HOST=127.0.0.1\nDB_PORT=3306\n"), 0o644))
	// Only git and a shell on PATH: php/composer/npm/yerd binary steps skip.
	bin := t.TempDir()
	for _, tool := range []string{"git", "sh"} {
		path, err := exec.LookPath(tool)
		require.NoError(t, err)
		require.NoError(t, os.Symlink(path, filepath.Join(bin, tool)))
	}
	// A stand-in php accepts artisan migrate/storage:link without touching data.
	require.NoError(t, os.WriteFile(filepath.Join(bin, "php"), []byte("#!/bin/sh\nexit 0\n"), 0o755))
	t.Setenv("PATH", bin)

	client := steps.NewMockDatabaseClient()
	registry := steps.NewRegistry()
	registry.RegisterDefaultsWithDatabaseFactory(steps.MockClientFactory(client))
	manager := NewScaffoldManagerWithRegistry(registry)
	preset := presets.NewManager(config.SiteDriverYerd).Resolve("laravel", "", dir)
	run := func() *PrepareReport {
		report, err := manager.RunScaffoldWithOptions(ScaffoldRequest{
			WorktreePath: dir, Branch: "feature", SiteName: "demo", Preset: preset.Name(),
			DefaultSteps: preset.DefaultSteps(), Config: &config.Config{}, Quiet: true, SkipCompleted: true,
		})
		require.NoError(t, err)
		return report
	}

	first := run()
	stateAfterFirst, err := config.ReadLocalState(dir)
	require.NoError(t, err)
	require.Len(t, stateAfterFirst.Databases, 2)

	second := run()
	upToDate := 0
	for i, step := range second.Steps {
		switch step.Name {
		case config.StepDbCreate, "db.migrate":
			require.Equal(t, OutcomeRan, step.Result, "%s must always run", step.Name)
		case config.StepFileCopy:
			require.Equal(t, OutcomeRan, first.Steps[i].Result)
			require.Equal(t, OutcomeSkipped, step.Result, ".env now exists, so the guard skips the copy")
		case config.StepEnvWrite:
			if step.Result == OutcomeUpToDate {
				upToDate++
			}
		}
	}
	require.Positive(t, upToDate, "recorded env.write steps are not repeated")
	stateAfterSecond, err := config.ReadLocalState(dir)
	require.NoError(t, err)
	require.Equal(t, stateAfterFirst.Databases, stateAfterSecond.Databases, "ownership records unchanged")
	require.Equal(t, stateAfterFirst.DbSuffix, stateAfterSecond.DbSuffix)
}
