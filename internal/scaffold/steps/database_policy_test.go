package steps

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/scaffold/types"
)

func TestDatabaseActionsValidateAllBeforeCommands(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("DB_CONNECTION=mysql\n"), 0600))
	require.NoError(t, config.WriteLocalState(root, config.LocalState{Databases: []config.OwnedDatabase{{Name: "app_one", Engine: "mysql", Endpoint: &config.DatabaseEndpoint{Driver: "yerd", Service: "mysql", Host: "127.0.0.1", Port: "3306"}, Role: "application"}, {Name: "test_two", Engine: "mysql", Endpoint: &config.DatabaseEndpoint{Driver: "yerd", Service: "mysql", Host: "127.0.0.1", Port: "3306"}, Role: "testing"}}}))
	ctx := &types.ScaffoldContext{WorktreePath: root, DatabaseActions: map[string]string{"application": "migrate", "missing": "migrate"}}
	_, err := planDatabaseActions(ctx)
	require.ErrorContains(t, err, "missing")
	ctx.DatabaseActions = map[string]string{"application": "skip", "app_one": "migrate"}
	_, err = planDatabaseActions(ctx)
	require.ErrorContains(t, err, "overlap")
	ctx.DatabaseActions = map[string]string{"testing": "migrate"}
	ctx.ResetDatabases = []string{"app_one"}
	actions, err := planDatabaseActions(ctx)
	require.NoError(t, err)
	require.Len(t, actions, 2)
	ctx.ResetDatabases = []string{"application"}
	_, err = planDatabaseActions(ctx)
	require.Error(t, err)
}
func TestDatabaseActionsSafeDefaultsAndIndependentTargets(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("DB_CONNECTION=mysql\n"), 0600))
	require.NoError(t, config.WriteLocalState(root, config.LocalState{Databases: []config.OwnedDatabase{{Name: "app", Engine: "mysql", Endpoint: &config.DatabaseEndpoint{Driver: "yerd", Service: "mysql", Host: "127.0.0.1", Port: "3306"}, Role: "application"}, {Name: "test", Engine: "mysql", Endpoint: &config.DatabaseEndpoint{Driver: "yerd", Service: "mysql", Host: "127.0.0.1", Port: "3306"}, Role: "testing"}}}))
	ctx := &types.ScaffoldContext{WorktreePath: root}
	actions, err := planDatabaseActions(ctx)
	require.NoError(t, err)
	require.Len(t, actions, 1)
	require.Equal(t, "skip", actions[0].Action)
	ctx.SetVar("ApplicationDatabaseNew", "true")
	actions, err = planDatabaseActions(ctx)
	require.NoError(t, err)
	require.Equal(t, "migrate", actions[0].Action)
	require.True(t, actions[0].Seed)
	ctx.DatabaseActions = map[string]string{"application": "migrate", "testing": "skip"}
	actions, err = planDatabaseActions(ctx)
	require.NoError(t, err)
	require.Len(t, actions, 2)
	require.False(t, actions[0].Seed)
}

func TestDatabaseActionsUseIndependentCommandEnvironments(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	root := t.TempDir()
	bin := t.TempDir()
	log := filepath.Join(root, "commands")
	original := []byte("DB_CONNECTION=mysql\nDB_DATABASE=app_one\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), original, 0600))
	require.NoError(t, os.WriteFile(filepath.Join(bin, "php"), []byte("#!/bin/sh\nprintf '%s|%s\\n' \"$DB_DATABASE\" \"$*\" >> \"$ANVIL_POLICY_LOG\"\n"), 0700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ANVIL_POLICY_LOG", log)
	require.NoError(t, config.WriteLocalState(root, config.LocalState{Databases: []config.OwnedDatabase{{Name: "app_one", Engine: "mysql", Endpoint: &config.DatabaseEndpoint{Driver: "yerd", Service: "mysql", Host: "127.0.0.1", Port: "3306"}, Role: "application"}, {Name: "test_two", Engine: "mysql", Endpoint: &config.DatabaseEndpoint{Driver: "yerd", Service: "mysql", Host: "127.0.0.1", Port: "3306"}, Role: "testing"}}}))
	ctx := &types.ScaffoldContext{WorktreePath: root, DatabaseActions: map[string]string{"application": "skip", "testing": "migrate"}}
	client := NewMockDatabaseClient()
	client.AddDatabase("app_one")
	client.AddDatabase("test_two")
	step := &DbMigrateStep{clientFactory: MockClientFactory(client)}
	require.NoError(t, step.Run(ctx, types.StepOptions{}))
	data, err := os.ReadFile(log)
	require.NoError(t, err)
	require.Equal(t, "test_two|artisan migrate --no-interaction\n", string(data))
	ctx.DatabaseActions = map[string]string{"application": "migrate", "missing": "migrate"}
	require.Error(t, step.Run(ctx, types.StepOptions{}))
	after, err := os.ReadFile(log)
	require.NoError(t, err)
	require.Equal(t, data, after)
	ctx.DatabaseActions = map[string]string{"application": "migrate"}
	ctx.ResetDatabases = []string{"test_two"}
	require.NoError(t, client.DropDatabase("test_two"))
	require.ErrorContains(t, step.Run(ctx, types.StepOptions{}), "absent")
	unchanged, err := os.ReadFile(log)
	require.NoError(t, err)
	require.Equal(t, data, unchanged)
	client.AddDatabase("test_two")
	require.NoError(t, step.Run(ctx, types.StepOptions{}))
	after, err = os.ReadFile(log)
	require.NoError(t, err)
	require.Contains(t, string(after), "app_one|artisan migrate --no-interaction\n")
	require.Contains(t, string(after), "test_two|artisan migrate:fresh --no-interaction --seed\n")
	env, err := os.ReadFile(filepath.Join(root, ".env"))
	require.NoError(t, err)
	require.Equal(t, original, env)
}

type idempotentMySQLClient struct{ *MockDatabaseClient }

func (c idempotentMySQLClient) CreateDatabase(name string) error {
	if c.HasDatabase(name) {
		return nil
	}
	return c.MockDatabaseClient.CreateDatabase(name)
}
func TestIdempotentMySQLAndLegacyExistingApplicationSkipMigration(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprint(legacy), func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("DB_CONNECTION=mysql\nDB_DATABASE=app_existing\n"), 0600))
			state := config.LocalState{DbSuffix: "existing"}
			if !legacy {
				state.Databases = []config.OwnedDatabase{{Name: "app_existing", Engine: "mysql", Endpoint: &config.DatabaseEndpoint{Driver: "yerd", Service: "mysql", Host: "127.0.0.1", Port: "3306"}, Role: "application"}}
			}
			require.NoError(t, config.WriteLocalState(root, state))
			client := idempotentMySQLClient{NewMockDatabaseClient()}
			client.AddDatabase("app_existing")
			factory := func(string, DatabaseOptions) (DatabaseClient, error) { return client, nil }
			ctx := &types.ScaffoldContext{WorktreePath: root, SiteName: "app", DbSuffix: "existing"}
			if legacy {
				ctx.SetDbSuffixLoadedFromLegacyState()
			}
			require.NoError(t, NewDbCreateStepWithFactory(config.StepConfig{}, factory).Run(ctx, types.StepOptions{}))
			require.Equal(t, "false", ctx.GetVar("ApplicationDatabaseNew"))
			actions, err := planDatabaseActions(ctx)
			require.NoError(t, err)
			require.Len(t, actions, 1)
			require.Equal(t, "skip", actions[0].Action)
		})
	}
}

func TestSharedDatabasePlanPreservesEndpointAndRejectsReset(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("DB_CONNECTION=mysql\nDB_DATABASE=shared_app\nDB_HOST=127.0.0.1\nDB_PORT=3307\n"), 0600))
	ctx := &types.ScaffoldContext{WorktreePath: root, DatabaseActions: map[string]string{"application": "migrate"}}
	actions, err := planDatabaseActions(ctx)
	require.NoError(t, err)
	require.Len(t, actions, 1)
	require.Equal(t, "127.0.0.1", actions[0].Host)
	require.Equal(t, "3307", actions[0].Port)
	ctx.DatabaseActions = nil
	ctx.ResetDatabases = []string{"shared_app"}
	_, err = planDatabaseActions(ctx)
	require.ErrorContains(t, err, "borrowed/shared")
}
