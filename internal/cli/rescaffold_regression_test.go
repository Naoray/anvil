package cli

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/presets"
	"github.com/naoray/anvil/internal/scaffold"
	"github.com/naoray/anvil/internal/scaffold/steps"
	"github.com/naoray/anvil/internal/scaffold/types"
)

type pgRegressionRegistry struct {
	factory steps.DatabaseClientFactory
}

func (r *pgRegressionRegistry) Create(name string, cfg config.StepConfig) (types.ScaffoldStep, error) {
	if name != config.StepDbCreate {
		return nil, fmt.Errorf("unexpected step %q in pg regression fixture", name)
	}
	return steps.NewDbCreateStepWithFactory(cfg, r.factory), nil
}

func (r *pgRegressionRegistry) ListRegistered() []string {
	return []string{config.StepDbCreate}
}

type pgRegressionPreset struct{}

func (pgRegressionPreset) Name() string       { return "pg-regression" }
func (pgRegressionPreset) Detect(string) bool { return false }
func (pgRegressionPreset) DefaultSteps() []config.StepConfig {
	return []config.StepConfig{
		{Name: config.StepDbCreate, Type: "pgsql"},
		{Name: config.StepDbCreate, Type: "pgsql", Role: config.DbRoleTesting},
	}
}
func (pgRegressionPreset) CleanupSteps() []config.CleanupStep { return nil }

// TestPostgresRescaffold_IdempotentEndToEnd is the CB2/P2-mandated cascade
// regression (PG-contract): a second scaffold over the same worktree must load
// the persisted suffix, treat both DatabaseExistsError results as idempotent
// success, keep exactly one application and one testing record with unchanged
// names, leave exec resolution working, and keep cleanup selection correct.
func TestPostgresRescaffold_IdempotentEndToEnd(t *testing.T) {
	dir := t.TempDir()
	client := steps.NewMockDatabaseClient()
	factory := steps.MockClientFactory(client)

	manager := scaffold.NewScaffoldManagerWithRegistry(&pgRegressionRegistry{factory: factory})
	presetManager := presets.NewManager()
	presetManager.Register(pgRegressionPreset{})
	resolvedPreset := presetManager.Resolve("pg-regression", "", dir)
	cfg := &config.Config{Preset: "pg-regression"}

	runScaffoldPass := func(pass int) {
		t.Helper()
		require.NoError(t,
			manager.RunScaffold(
				dir,
				"feature-x",
				"repo",
				"demo",
				resolvedPreset.Name(),
				resolvedPreset.DefaultSteps(),
				cfg,
				false,
				false,
				true,
			),
			"scaffold pass %d", pass)
	}

	// PASS 1: fresh dir, fresh suffix, both creates succeed on the mock.
	runScaffoldPass(1)
	stateAfterFirst, err := config.ReadLocalState(dir)
	require.NoError(t, err)
	suffix := stateAfterFirst.DbSuffix
	require.NotEmpty(t, suffix)
	require.Len(t, stateAfterFirst.Databases, 2)

	// PASS 2: same dir and flow; the suffix loads from state and both creates
	// hit DatabaseExistsError (the mock already holds the databases — only the
	// PostgreSQL client produces this error in production).
	runScaffoldPass(2)
	stateAfterSecond, err := config.ReadLocalState(dir)
	require.NoError(t, err)

	assert.Equal(t, suffix, stateAfterSecond.DbSuffix, "re-scaffold must not rotate the persisted suffix")
	require.Len(t, stateAfterSecond.Databases, 2, "re-scaffold must not accumulate ownership records")
	assert.Equal(t, stateAfterFirst.Databases, stateAfterSecond.Databases, "record names must be unchanged")

	var appRecord, testRecord config.OwnedDatabase
	roleCounts := make(map[string]int)
	for _, record := range stateAfterSecond.Databases {
		roleCounts[record.Role]++
		switch record.Role {
		case config.DbRoleApplication:
			appRecord = record
		case config.DbRoleTesting:
			testRecord = record
		}
	}
	assert.Equal(t, map[string]int{config.DbRoleApplication: 1, config.DbRoleTesting: 1}, roleCounts)

	// No rotation anywhere: pass 2 retried the exact pass-1 names.
	assert.Equal(t,
		[]string{appRecord.Name, testRecord.Name, appRecord.Name, testRecord.Name},
		client.GetCreateCalls())

	// exec is not bricked after the re-scaffold.
	appDb, testDb, err := resolveExecDatabases(dir)
	require.NoError(t, err)
	assert.Equal(t, appRecord.Name, appDb)
	assert.Equal(t, testRecord.Name, testDb)

	// Cleanup selection stays correct: exact records plus scripted worker
	// families enumerate with zero drops under dry-run.
	client.SetListResultsForPattern(
		steps.EscapeLikePattern(appRecord.Name)+`\_test\_%`,
		[]string{appRecord.Name + "_test_1", appRecord.Name + "_test_2"},
	)
	output := &bytes.Buffer{}
	destroyStep := steps.NewDbDestroyStepWithFactoryAndWriter(
		config.StepConfig{Name: config.StepDbDestroy}, factory, output)
	ctx := &types.ScaffoldContext{WorktreePath: dir}
	require.NoError(t, destroyStep.Run(ctx, types.StepOptions{DryRun: true, Quiet: true}))

	expected := fmt.Sprintf(
		"Would drop database: %s\nWould drop database: %s\nWould drop database: %s\nWould drop database: %s\n",
		appRecord.Name, testRecord.Name, appRecord.Name+"_test_1", appRecord.Name+"_test_2")
	assert.Equal(t, expected, output.String())
	assert.Empty(t, client.GetDropCalls(), "dry-run selection must never drop")
}
