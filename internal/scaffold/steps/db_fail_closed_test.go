package steps

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/scaffold/types"
)

// A configured server engine whose connection check fails must fail the
// step: continuing would let scaffolding report a worktree without its
// database as ready.
func TestDbCreateStep_ConfiguredEnginePingFailureFailsClosed(t *testing.T) {
	cases := []struct {
		name   string
		dbType string
		env    string
		engine string
	}{
		{name: "mysql type", dbType: "mysql", engine: "mysql"},
		{name: "mariadb type", dbType: "mariadb", engine: "mysql"},
		{name: "pgsql type", dbType: "pgsql", engine: "pgsql"},
		{name: "mysql env", env: "DB_CONNECTION=mysql\n", engine: "mysql"},
		{name: "mariadb env", env: "DB_CONNECTION=mariadb\n", engine: "mysql"},
		{name: "pgsql env", env: "DB_CONNECTION=pgsql\n", engine: "pgsql"},
		{name: "postgres env", env: "DB_CONNECTION=postgres\n", engine: "pgsql"},
	}
	for _, role := range []string{config.DbRoleApplication, config.DbRoleTesting} {
		for _, tc := range cases {
			t.Run(role+"/"+tc.name, func(t *testing.T) {
				dir := t.TempDir()
				if tc.env != "" {
					require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(tc.env), 0o644))
				}
				client := NewMockDatabaseClient()
				client.SetPingError(errors.New("connection refused"))
				step := NewDbCreateStepWithFactory(config.StepConfig{Type: tc.dbType, Role: role}, MockClientFactory(client))
				ctx := &types.ScaffoldContext{WorktreePath: dir, SiteName: "Dashboard"}
				ctx.SetDbSuffix("top_provider")

				err := step.Run(ctx, types.StepOptions{})
				require.Error(t, err)
				assert.ErrorContains(t, err, "connection refused")
				assert.ErrorContains(t, err, tc.engine+" database")
				assert.ErrorContains(t, err, role+" role")
				assert.ErrorContains(t, err, "start the database service or check its connection settings")
				assert.Empty(t, client.GetCreateCalls())
				state, err := config.ReadLocalState(dir)
				require.NoError(t, err)
				assert.Empty(t, state.Databases, "no ownership recorded for a database that was never created")
			})
		}
	}
}

func TestDbCreateStep_ManagedServiceFailureKeepsContextAndHint(t *testing.T) {
	client := NewMockDatabaseClient()
	client.SetPingError(newManagedServiceError("Yerd service mysql is not installed"))
	step := NewDbCreateStepWithFactory(config.StepConfig{Type: "mysql"}, MockClientFactory(client))
	ctx := &types.ScaffoldContext{WorktreePath: t.TempDir(), SiteName: "Dashboard"}
	ctx.SetDbSuffix("top_provider")

	err := step.Run(ctx, types.StepOptions{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "preparing managed database service")
	assert.ErrorContains(t, err, "Yerd service mysql is not installed")
	assert.ErrorContains(t, err, "start the database service or check its connection settings")
}

func TestDbCreateStep_UnsupportedExplicitTypeFailsClosed(t *testing.T) {
	factory := func(string, DatabaseOptions) (DatabaseClient, error) {
		t.Fatal("no client may be created for an unsupported engine")
		return nil, nil
	}
	step := NewDbCreateStepWithFactory(config.StepConfig{Type: "mssql"}, factory)
	ctx := &types.ScaffoldContext{WorktreePath: t.TempDir(), SiteName: "Dashboard"}
	ctx.SetDbSuffix("top_provider")

	err := step.Run(ctx, types.StepOptions{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "unsupported database type: mssql")
}

// Without an explicit type or DB_CONNECTION the project has not configured a
// database engine, so the step remains a no-op.
func TestDbCreateStep_UndeterminableEngineIsNoOp(t *testing.T) {
	for _, env := range []string{"", "APP_NAME=demo\n", "DB_CONNECTION=sqlsrv\n"} {
		dir := t.TempDir()
		if env != "" {
			require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(env), 0o644))
		}
		factory := func(string, DatabaseOptions) (DatabaseClient, error) {
			t.Fatal("no client may be created without a configured engine")
			return nil, nil
		}
		for _, role := range []string{config.DbRoleApplication, config.DbRoleTesting} {
			step := NewDbCreateStepWithFactory(config.StepConfig{Role: role}, factory)
			ctx := &types.ScaffoldContext{WorktreePath: dir, SiteName: "Dashboard"}
			ctx.SetDbSuffix("top_provider")
			require.NoError(t, step.Run(ctx, types.StepOptions{}), "env %q role %s", env, role)
		}
	}
}

func TestDbCreateStep_ExplicitTypeAliases(t *testing.T) {
	cases := []struct {
		dbType  string
		engine  string
		service string
	}{
		{"postgres", "pgsql", ""},
		{"PostgreSQL", "pgsql", ""},
		{"MySQL", "mysql", ""},
		{"MariaDB", "mysql", "mariadb"},
	}
	for _, tc := range cases {
		t.Run(tc.dbType, func(t *testing.T) {
			dir := t.TempDir()
			client := NewMockDatabaseClient()
			var gotEngine string
			var gotOptions DatabaseOptions
			mockFactory := MockClientFactory(client)
			factory := func(engine string, options DatabaseOptions) (DatabaseClient, error) {
				gotEngine, gotOptions = engine, options
				return mockFactory(engine, options)
			}
			step := NewDbCreateStepWithFactory(config.StepConfig{Type: tc.dbType}, factory)
			ctx := &types.ScaffoldContext{WorktreePath: dir, SiteName: "Dashboard"}
			ctx.SetDbSuffix("top_provider")

			require.NoError(t, step.Run(ctx, types.StepOptions{}))
			assert.Equal(t, tc.engine, gotEngine)
			assert.Equal(t, tc.service, gotOptions.Service)
			assert.Equal(t, []string{"dashboard_top_provider"}, client.GetCreateCalls())
		})
	}
}

func TestDbDestroyStep_ExplicitTypeAliases(t *testing.T) {
	for _, dbType := range []string{"postgres", "MySQL"} {
		t.Run(dbType, func(t *testing.T) {
			client := NewMockDatabaseClient()
			client.AddDatabase("dashboard_top_provider")
			step := NewDbDestroyStepWithFactory(config.StepConfig{Type: dbType}, MockClientFactory(client))
			ctx := &types.ScaffoldContext{WorktreePath: t.TempDir()}
			ctx.SetDbSuffix("top_provider")

			require.NoError(t, step.Run(ctx, types.StepOptions{}))
			assert.Equal(t, []string{"dashboard_top_provider"}, client.GetDropCalls())
		})
	}
}
