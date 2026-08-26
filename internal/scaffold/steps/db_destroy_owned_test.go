package steps

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/scaffold/types"
)

func TestDbDestroyStep_InvalidOwnedStateFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		dbs  []config.OwnedDatabase
		want string
	}{
		{"unknown role", []config.OwnedDatabase{
			{Name: "app", Engine: "pgsql", Role: config.DbRoleApplication},
			{Name: "weird", Engine: "pgsql", Role: "someday-role"},
		}, "unsupported database record role"},
		{"unknown engine", []config.OwnedDatabase{
			{Name: "app", Engine: "oracle", Role: config.DbRoleApplication},
		}, "unsupported database engine"},
		{"mixed engines", []config.OwnedDatabase{
			{Name: "app", Engine: "mysql", Role: config.DbRoleApplication},
			{Name: "app_test", Engine: "pgsql", Role: config.DbRoleTesting},
		}, "exactly one supported engine"},
		{"invalid name", []config.OwnedDatabase{
			{Name: "bad;drop", Engine: "mysql", Role: config.DbRoleApplication},
		}, "invalid database name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeOwnedState(t, dir, tt.dbs)
			client := NewMockDatabaseClient()
			factory := NewMockClientFactoryRecorder(client)
			step := NewDbDestroyStepWithFactory(config.StepConfig{}, factory.Factory)

			err := step.Run(&types.ScaffoldContext{WorktreePath: dir}, types.StepOptions{})
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
			assert.Equal(t, 0, factory.CallCount())
			assert.Empty(t, client.GetListCalls())
			assert.Empty(t, client.GetDropCalls())
		})
	}
}

func TestDbDestroyStep_InvalidOwnedStateAggregatesEveryViolation(t *testing.T) {
	dir := t.TempDir()
	writeOwnedState(t, dir, []config.OwnedDatabase{
		{Name: "bad;first", Engine: "mysql", Role: "someday-role"},
		{Name: "second", Engine: "oracle", Role: config.DbRoleTesting},
		{Name: "third", Engine: "pgsql", Role: config.DbRoleApplication},
	})
	factory := NewMockClientFactoryRecorder(NewMockDatabaseClient())
	step := NewDbDestroyStepWithFactory(config.StepConfig{}, factory.Factory)
	err := step.Run(&types.ScaffoldContext{WorktreePath: dir}, types.StepOptions{})
	require.Error(t, err)
	for _, want := range []string{"bad;first", "someday-role", "second", "oracle", "exactly one supported engine"} {
		assert.ErrorContains(t, err, want)
	}
	assert.Equal(t, 0, factory.CallCount())
}

func TestDbDestroyStep_DuplicateOwnedStateFailsBeforeClient(t *testing.T) {
	dir := t.TempDir()
	writeRawOwnedState(t, dir, `db_suffix: top_provider
databases:
  - name: app_top_provider
    engine: mysql
    role: application
  - name: app_top_provider
    engine: mysql
    role: testing
  - name: worker_top_provider_test
    engine: mysql
    role: testing
`)
	client := NewMockDatabaseClient()
	factory := NewMockClientFactoryRecorder(client)
	step := NewDbDestroyStepWithFactory(config.StepConfig{}, factory.Factory)

	err := step.Run(&types.ScaffoldContext{WorktreePath: dir}, types.StepOptions{})
	require.Error(t, err)
	assert.ErrorContains(t, err, `duplicate database name "app_top_provider" in record 1; first seen in record 0`)
	assert.ErrorContains(t, err, `duplicate database role "testing" in record 2; first seen in record 1`)
	assert.Equal(t, 0, factory.CallCount())
	assert.Empty(t, client.GetListCalls())
	assert.Empty(t, client.GetDropCalls())
}

func TestDbDestroyStep_OwnedStateDropsExactAndBothWorkerFamilies(t *testing.T) {
	dir := t.TempDir()
	app := "dashboard_top_provider"
	testDB := "dashboard_top_provider_test"
	writeOwnedState(t, dir, []config.OwnedDatabase{
		{Name: app, Engine: "mysql", Role: config.DbRoleApplication},
		{Name: testDB, Engine: "mysql", Role: config.DbRoleTesting},
	})
	client := NewMockDatabaseClient()
	for _, name := range []string{
		app, testDB,
		testDB + "_1",
		testDB + "_test_1",
		testDB + "_test_2",
		"dashboard_top_provider2",
		"dashboard_atop_provider",
	} {
		client.AddDatabase(name)
	}
	step := NewDbDestroyStepWithFactory(config.StepConfig{}, MockClientFactory(client))
	require.NoError(t, step.Run(&types.ScaffoldContext{WorktreePath: dir}, types.StepOptions{}))

	assert.Equal(t, []string{
		app,
		testDB,
		testDB + "_1",
		testDB + "_test_1",
		testDB + "_test_2",
	}, client.GetDropCalls())
	assert.Equal(t, []string{
		EscapeLikePattern(app) + `\_test\_%`,
		EscapeLikePattern(testDB) + `\_test\_%`,
	}, client.GetListCalls())
	assert.True(t, client.HasDatabase("dashboard_top_provider2"))
	assert.True(t, client.HasDatabase("dashboard_atop_provider"))
}

func TestDbDestroyStep_WorkerCandidatesPrefixRevalidated(t *testing.T) {
	dir := t.TempDir()
	app := "app_top_provider"
	writeOwnedState(t, dir, []config.OwnedDatabase{{
		Name: app, Engine: "pgsql", Role: config.DbRoleApplication,
	}})
	client := NewMockDatabaseClient()
	pattern := EscapeLikePattern(app) + `\_test\_%`
	client.SetListResultsForPattern(pattern, []string{
		app + "_test_1",
		"evil;name",
		"unrelated_db",
	})
	step := NewDbDestroyStepWithFactory(config.StepConfig{}, MockClientFactory(client))
	require.NoError(t, step.Run(&types.ScaffoldContext{WorktreePath: dir}, types.StepOptions{}))
	assert.Equal(t, []string{app, app + "_test_1"}, client.GetDropCalls())
}

func TestDbDestroyStep_RuntimeErrorsAggregatedNotSwallowed(t *testing.T) {
	dir := t.TempDir()
	app := "app_top_provider"
	testDB := "app_top_provider_test"
	writeOwnedState(t, dir, []config.OwnedDatabase{
		{Name: app, Engine: "mysql", Role: config.DbRoleApplication},
		{Name: testDB, Engine: "mysql", Role: config.DbRoleTesting},
	})
	listErr := errors.New("list app workers")
	dropErr := errors.New("drop app")
	closeErr := errors.New("close client")
	client := NewMockDatabaseClient()
	client.SetListErrorForPattern(EscapeLikePattern(app)+`\_test\_%`, listErr)
	client.SetDropErrorForDatabase(app, dropErr)
	client.SetCloseError(closeErr)
	step := NewDbDestroyStepWithFactory(config.StepConfig{}, MockClientFactory(client))

	err := step.Run(&types.ScaffoldContext{WorktreePath: dir}, types.StepOptions{})
	require.Error(t, err)
	assert.ErrorIs(t, err, listErr)
	assert.ErrorIs(t, err, dropErr)
	assert.ErrorIs(t, err, closeErr)
	assert.Equal(t, []string{app, testDB}, client.GetDropCalls())
}

func TestDbDestroyStep_DryRunEnumeratesWithoutDrops(t *testing.T) {
	dir := t.TempDir()
	app := "app_top_provider"
	writeOwnedState(t, dir, []config.OwnedDatabase{{
		Name: app, Engine: "mysql", Role: config.DbRoleApplication,
	}})
	client := NewMockDatabaseClient()
	client.AddDatabase(app + "_test_1")
	step := NewDbDestroyStepWithFactory(config.StepConfig{}, MockClientFactory(client))
	require.NoError(t, step.Run(&types.ScaffoldContext{WorktreePath: dir}, types.StepOptions{DryRun: true}))
	assert.Empty(t, client.GetDropCalls())
	assert.NotEmpty(t, client.GetListCalls())
}

func TestDbDestroyStep_LegacyStateEscapedAndValidated(t *testing.T) {
	t.Run("escaped suffix and returned names", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, config.WriteLocalState(dir, config.LocalState{DbSuffix: "top_provider"}))
		client := NewMockDatabaseClient()
		client.AddDatabase("dashboard_top_provider")
		client.AddDatabase("dashboard_topXprovider")
		step := NewDbDestroyStepWithFactory(config.StepConfig{Type: "mysql"}, MockClientFactory(client))
		require.NoError(t, step.Run(&types.ScaffoldContext{WorktreePath: dir}, types.StepOptions{}))
		assert.Equal(t, []string{`%\_top\_provider`}, client.GetListCalls())
		assert.Equal(t, []string{"dashboard_top_provider"}, client.GetDropCalls())
	})

	t.Run("invalid suffix fails before factory", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, config.WriteLocalState(dir, config.LocalState{DbSuffix: "bad;suffix"}))
		factory := NewMockClientFactoryRecorder(NewMockDatabaseClient())
		step := NewDbDestroyStepWithFactory(config.StepConfig{Type: "mysql"}, factory.Factory)
		err := step.Run(&types.ScaffoldContext{WorktreePath: dir}, types.StepOptions{})
		require.Error(t, err)
		assert.Contains(t, strings.ToLower(err.Error()), "invalid")
		assert.Equal(t, 0, factory.CallCount())
	})
}

func writeOwnedState(t *testing.T, dir string, databases []config.OwnedDatabase) {
	t.Helper()
	require.NoError(t, config.WriteLocalState(dir, config.LocalState{
		DbSuffix: "top_provider", Databases: databases,
	}))
}

func writeRawOwnedState(t *testing.T, dir, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.LocalStateFile), []byte(content), 0o644))
}
