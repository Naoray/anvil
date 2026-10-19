package steps

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/scaffold/types"
)

func TestUnknownOwnedIdentityCannotAuthorizeReset(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("DB_CONNECTION=mysql\n"), 0o600))
	require.NoError(t, config.WriteLocalState(root, config.LocalState{Databases: []config.OwnedDatabase{{Name: "owned_app", Engine: "mysql", Role: "application"}}}))
	client := NewMockDatabaseClient()
	client.AddDatabase("owned_app")
	ctx := &types.ScaffoldContext{WorktreePath: root, ResetDatabases: []string{"owned_app"}}
	err := (&DbMigrateStep{clientFactory: MockClientFactory(client)}).Run(ctx, types.StepOptions{DryRun: true})
	require.ErrorContains(t, err, "identity")
}

func TestRecordedEndpointSurvivesEnvironmentDrift(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("DB_CONNECTION=mariadb\nDB_HOST=127.0.0.1\nDB_PORT=3307\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".anvil.local"), []byte(`databases:
  - name: owned_app
    engine: mysql
    role: application
    endpoint: {driver: herd, service: mysql, host: 127.0.0.1, port: "3306"}
`), 0o600))
	ctx := &types.ScaffoldContext{WorktreePath: root, SiteDriver: "yerd", ResetDatabases: []string{"owned_app"}}
	actions, err := planDatabaseActions(ctx)
	require.NoError(t, err)
	require.Equal(t, "mysql", actions[0].Service)
	require.Equal(t, "3306", actions[0].Port)
}

func TestPreexistingUnownedTestingDatabaseIsNeverAdopted(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("DB_CONNECTION=mysql\n"), 0o600))
	require.NoError(t, config.WriteLocalState(root, config.LocalState{DbSuffix: "suffix", Databases: []config.OwnedDatabase{{Name: "app_suffix", Engine: "mysql", Role: "application"}}}))
	before, err := os.ReadFile(filepath.Join(root, ".anvil.local"))
	require.NoError(t, err)
	client := idempotentMySQLClient{NewMockDatabaseClient()}
	client.AddDatabase("app_suffix_test")
	factory := func(string, DatabaseOptions) (DatabaseClient, error) { return client, nil }
	ctx := &types.ScaffoldContext{WorktreePath: root, SiteName: "app", DbSuffix: "suffix"}
	err = NewDbCreateStepWithFactory(config.StepConfig{Role: "testing"}, factory).Run(ctx, types.StepOptions{})
	require.ErrorContains(t, err, "not owned")
	require.Empty(t, client.GetCreateCalls())
	require.True(t, client.HasDatabase("app_suffix_test"))
	after, err := os.ReadFile(filepath.Join(root, ".anvil.local"))
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestFreshDatabaseDryRunPlansWithoutPhysicalCreation(t *testing.T) {
	for _, engine := range []string{"mysql", "sqlite"} {
		t.Run(engine, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("DB_CONNECTION="+engine+"\n"), 0o600))
			ctx := &types.ScaffoldContext{WorktreePath: root, SiteName: "app", DbSuffix: "suffix"}
			client := NewMockDatabaseClient()
			factory := MockClientFactory(client)
			require.NoError(t, NewDbCreateStepWithFactory(config.StepConfig{}, factory).Run(ctx, types.StepOptions{DryRun: true}))
			require.NoError(t, (&DbMigrateStep{clientFactory: factory}).Run(ctx, types.StepOptions{DryRun: true}))
			require.Empty(t, client.GetCreateCalls())
			require.NoFileExists(t, filepath.Join(root, ".anvil.local"))
			require.NoFileExists(t, filepath.Join(root, "database/database.sqlite"))
		})
	}
}

func TestYerdAvailabilityAndArtisanMustUseSameEndpoint(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX command fixtures")
	}
	root := t.TempDir()
	bin := t.TempDir()
	log := filepath.Join(root, "php-log")
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("DB_CONNECTION=mysql\nDB_HOST=127.0.0.1\nDB_PORT=3307\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".anvil.local"), []byte(`databases:
  - name: owned_app
    engine: mysql
    role: application
    endpoint: {driver: yerd, service: mysql, host: 127.0.0.1, port: "3307"}
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(bin, "yerd"), []byte("#!/bin/sh\ncase \"$*\" in *services*) echo '{\"services\":[{\"service\":\"mysql\",\"state\":\"running\",\"listen\":\"127.0.0.1:3306\",\"port\":3306}]}' ;; *) echo '{\"databases\":[{\"name\":\"owned_app\"}]}' ;; esac\n"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(bin, "php"), []byte("#!/bin/sh\necho reached > \"$ANVIL_IDENTITY_PHP_LOG\"\n"), 0o700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ANVIL_IDENTITY_PHP_LOG", log)
	err := (&DbMigrateStep{}).Run(&types.ScaffoldContext{WorktreePath: root, SiteDriver: "yerd", ResetDatabases: []string{"owned_app"}}, types.StepOptions{})
	require.ErrorContains(t, err, "endpoint")
	require.NoFileExists(t, log)
}

func TestUnknownOwnedIdentityCannotBeBorrowed(t *testing.T) {
	root := t.TempDir()
	owner := filepath.Join(root, "owner")
	target := filepath.Join(root, "target")
	output, err := exec.Command("git", "init", owner).CombinedOutput()
	require.NoError(t, err, string(output))
	output, err = exec.Command("git", "-C", owner, "worktree", "add", "--orphan", target).CombinedOutput()
	require.NoError(t, err, string(output))
	for _, path := range []string{owner, target} {
		require.NoError(t, os.WriteFile(filepath.Join(path, ".env"), []byte("DB_CONNECTION=mysql\n"), 0o600))
	}
	require.NoError(t, config.WriteLocalState(owner, config.LocalState{Databases: []config.OwnedDatabase{{Name: "legacy_app", Engine: "mysql", Role: "application"}}}))
	err = NewDbCreateStepWithFactory(config.StepConfig{}, MockClientFactory(NewMockDatabaseClient())).Run(&types.ScaffoldContext{WorktreePath: target, BorrowAppDB: owner}, types.StepOptions{})
	require.ErrorContains(t, err, "identity")
	require.NoFileExists(t, filepath.Join(target, ".anvil.local"))
}

func TestCleanupUsesRecordedDriverAndEndpointAfterConfigurationChange(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("DB_CONNECTION=mariadb\nDB_HOST=127.0.0.1\nDB_PORT=3307\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".anvil.local"), []byte(`databases:
  - name: historical_app
    engine: mysql
    role: auxiliary
    endpoint: {driver: herd, service: mysql, host: 127.0.0.1, port: "3306"}
`), 0o600))
	commander := &yerdRecordingCommander{output: []byte("historical_app\n")}
	registry := NewRegistry()
	registry.RegisterDefaultsForSiteDriver(config.SiteDriverYerd, commander)
	step, err := registry.Create(config.StepDbDestroy, config.StepConfig{})
	require.NoError(t, err)
	require.NoError(t, step.Run(&types.ScaffoldContext{WorktreePath: root}, types.StepOptions{}))
	dropped := false
	for _, call := range commander.calls {
		require.NotEqual(t, "yerd", call.command)
		if call.command == "mysql" {
			require.Contains(t, call.args, "--host=127.0.0.1")
			require.Contains(t, call.args, "--port=3306")
			require.NotContains(t, call.args, "--port=3307")
			if slices.Contains(call.args, "DROP DATABASE IF EXISTS `historical_app`") {
				dropped = true
			}
		}
	}
	require.True(t, dropped)
}

func TestLegacyOwnedIdentityCannotAuthorizeCleanup(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, config.WriteLocalState(root, config.LocalState{Databases: []config.OwnedDatabase{{Name: "legacy_app", Engine: "mysql", Role: "application"}}}))
	client := NewMockDatabaseClient()
	client.AddDatabase("legacy_app")
	err := NewDbDestroyStepWithFactory(config.StepConfig{}, MockClientFactory(client)).Run(&types.ScaffoldContext{WorktreePath: root}, types.StepOptions{})
	require.ErrorContains(t, err, "identity")
	require.Empty(t, client.GetDropCalls())
	require.True(t, client.HasDatabase("legacy_app"))
}

func TestMissingExistingResetStillFailsDuringDryRun(t *testing.T) {
	root := t.TempDir()
	endpoint := &config.DatabaseEndpoint{Driver: "yerd", Service: "mysql", Host: "127.0.0.1", Port: "3306"}
	require.NoError(t, config.WriteLocalState(root, config.LocalState{Databases: []config.OwnedDatabase{{Name: "missing_app", Engine: "mysql", Role: "application", Endpoint: endpoint}}}))
	client := NewMockDatabaseClient()
	err := (&DbMigrateStep{clientFactory: MockClientFactory(client)}).Run(&types.ScaffoldContext{WorktreePath: root, ResetDatabases: []string{"missing_app"}}, types.StepOptions{DryRun: true})
	require.ErrorContains(t, err, "absent")
	require.Empty(t, client.GetCreateCalls())
}

func TestBorrowedMigrationUsesRecordedDriverAndCommandEndpoint(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX command fixture")
	}
	root := t.TempDir()
	bin := t.TempDir()
	log := filepath.Join(root, "command")
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("DB_CONNECTION=mariadb\nDB_HOST=127.0.0.1\nDB_PORT=3307\n"), 0o600))
	binding := &config.BorrowedApplication{SourcePath: filepath.Join(root, "owner"), Name: "borrowed_app", Engine: "mysql", Driver: "herd", Service: "mysql", Host: "127.0.0.1", Port: "3306"}
	require.NoError(t, config.WriteLocalState(root, config.LocalState{BorrowedApplication: binding}))
	require.NoError(t, os.WriteFile(filepath.Join(bin, "php"), []byte("#!/bin/sh\nprintf '%s|%s|%s' \"$DB_DATABASE\" \"$DB_CONNECTION\" \"$DB_PORT\" > \"$ANVIL_IDENTITY_PHP_LOG\"\n"), 0o700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ANVIL_IDENTITY_PHP_LOG", log)
	commander := &yerdRecordingCommander{output: []byte("borrowed_app\n")}
	step := &DbMigrateStep{clientFactory: managedDatabaseFactory(config.SiteDriverYerd, commander)}
	require.NoError(t, step.Run(&types.ScaffoldContext{WorktreePath: root, SiteDriver: "yerd", DatabaseActions: map[string]string{"application": "migrate"}}, types.StepOptions{}))
	data, err := os.ReadFile(log)
	require.NoError(t, err)
	require.Equal(t, "borrowed_app|mysql|3306", string(data))
	for _, call := range commander.calls {
		require.NotEqual(t, "yerd", call.command)
		if call.command == "mysql" {
			require.Contains(t, call.args, "--port=3306")
		}
	}
}

func TestHerdAvailabilityUsesTheReportedDefaultEndpoint(t *testing.T) {
	for _, engine := range []string{"mysql", "pgsql"} {
		t.Run(engine, func(t *testing.T) {
			commander := &yerdRecordingCommander{}
			client, err := NewHerdDatabaseClientFactory(commander)(engine, DatabaseOptions{WorktreePath: t.TempDir()})
			require.NoError(t, err)
			endpoint, err := client.Endpoint()
			require.NoError(t, err)
			_, err = client.ListDatabases("%")
			require.NoError(t, err)
			require.Len(t, commander.calls, 1)
			require.Contains(t, commander.calls[0].args, "--port="+endpoint.Port)
		})
	}
}

func TestLegacyNameCannotBeReattestedOnAnotherUnprovenEndpoint(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("DB_CONNECTION=mysql\n"), 0o600))
	require.NoError(t, config.WriteLocalState(root, config.LocalState{Databases: []config.OwnedDatabase{{Name: "app_suffix", Engine: "mysql", Role: "application"}}}))
	before, err := os.ReadFile(filepath.Join(root, ".anvil.local"))
	require.NoError(t, err)
	client := NewMockDatabaseClient()
	err = NewDbCreateStepWithFactory(config.StepConfig{}, MockClientFactory(client)).Run(&types.ScaffoldContext{WorktreePath: root, SiteName: "app", DbSuffix: "suffix"}, types.StepOptions{})
	require.ErrorContains(t, err, "identity")
	require.Empty(t, client.GetCreateCalls())
	after, err := os.ReadFile(filepath.Join(root, ".anvil.local"))
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestHerdLocalhostUsesRecordedTCPTransport(t *testing.T) {
	for _, engine := range []string{"mysql", "mariadb"} {
		for _, source := range []string{"env", "argument"} {
			t.Run(engine+"/"+source, func(t *testing.T) {
				root := t.TempDir()
				env := "DB_CONNECTION=" + engine + "\nDB_HOST=localhost\nDB_PORT=3307\n"
				cfg := config.StepConfig{}
				if source == "argument" {
					env = "DB_CONNECTION=" + engine + "\nDB_HOST=127.0.0.1\nDB_PORT=3306\n"
					cfg.Args = []string{"--host=localhost", "--port=3307"}
				}
				require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte(env), 0o600))
				commander := &yerdRecordingCommander{}
				factory := NewHerdDatabaseClientFactory(commander)
				ctx := &types.ScaffoldContext{WorktreePath: root, SiteName: "app", DbSuffix: "suffix", SiteDriver: "herd"}
				require.NoError(t, NewDbCreateStepWithFactory(cfg, factory).Run(ctx, types.StepOptions{}))
				state, err := config.ReadLocalState(root)
				require.NoError(t, err)
				require.Equal(t, &config.DatabaseEndpoint{Driver: "herd", Service: engine, Host: "127.0.0.1", Port: "3307"}, state.Databases[0].Endpoint)
				// Drift back to the default-port socket spelling; migration must keep the created identity.
				require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("DB_CONNECTION="+engine+"\nDB_HOST=localhost\nDB_PORT=3306\n"), 0o600))
				actions, err := planDatabaseActions(ctx)
				require.NoError(t, err)
				require.Equal(t, "127.0.0.1", actions[0].Host)
				require.Equal(t, "3307", actions[0].Port)
				client, err := factory(engine, DatabaseOptions{WorktreePath: root, Endpoint: state.Databases[0].Endpoint})
				require.NoError(t, err)
				commander.output = []byte("app_suffix\n")
				names, err := client.ListDatabases("app_suffix")
				require.NoError(t, err)
				require.Equal(t, []string{"app_suffix"}, names)
				require.NoError(t, client.DropDatabase("app_suffix"))
				require.NoError(t, client.Close())
				var create, list, drop bool
				for _, call := range commander.calls {
					if call.command != engine {
						continue
					}
					require.Contains(t, call.args, "--host=127.0.0.1")
					require.Contains(t, call.args, "--port=3307")
					require.NotContains(t, call.args, "--host=localhost")
					create = create || slices.Contains(call.args, "CREATE DATABASE `app_suffix`")
					list = list || slices.Contains(call.args, "SHOW DATABASES")
					drop = drop || slices.Contains(call.args, "DROP DATABASE IF EXISTS `app_suffix`")
				}
				require.True(t, create && list && drop)
			})
		}
	}
}

func TestHerdPostgreSQLPreservesSocketHost(t *testing.T) {
	commander := &yerdRecordingCommander{}
	client, err := NewHerdDatabaseClientFactory(commander)("pgsql", DatabaseOptions{WorktreePath: t.TempDir(), Host: "/tmp/private-postgres", Port: "5437"})
	require.NoError(t, err)
	endpoint, err := client.Endpoint()
	require.NoError(t, err)
	require.Equal(t, "/tmp/private-postgres", endpoint.Host)
	require.NoError(t, client.CreateDatabase("socket_app"))
	_, err = client.ListDatabases("%")
	require.NoError(t, err)
	require.NoError(t, client.DropDatabase("socket_app"))
	for _, call := range commander.calls {
		require.Contains(t, call.args, "--host=/tmp/private-postgres")
		require.Contains(t, call.args, "--port=5437")
	}
}
