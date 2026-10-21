package steps

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/dblifecycle"
	"github.com/naoray/anvil/internal/scaffold/types"
)

const (
	testingFixtureSuffix = "brave_otter"
	testingFixtureApp    = "demo_brave_otter"
	testingFixtureTest   = "demo_brave_otter_test"
)

var testingFixtureEndpoint = config.DatabaseEndpoint{Driver: "yerd", Service: "mysql", Host: "127.0.0.1", Port: "3306"}

// testingFixtureContext mirrors the context db.create leaves behind after
// ensuring the worktree's application and testing databases.
func testingFixtureContext(root string, testingNew string) *types.ScaffoldContext {
	ctx := &types.ScaffoldContext{WorktreePath: root, SiteName: "demo", DbSuffix: testingFixtureSuffix}
	ctx.SetVar("DatabaseName", testingFixtureApp)
	ctx.SetVar("ApplicationDatabaseNew", "false")
	ctx.SetVar("TestDatabaseName", testingFixtureTest)
	if testingNew != "" {
		ctx.SetVar("DatabaseNew:"+testingFixtureTest, testingNew)
	}
	return ctx
}

func writeTestingFixture(t *testing.T, root string, databases ...config.OwnedDatabase) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("DB_CONNECTION=mysql\n"), 0o600))
	require.NoError(t, config.WriteLocalState(root, config.LocalState{DbSuffix: testingFixtureSuffix, Databases: databases}))
}

func ownedFixtureDatabase(name, role string) config.OwnedDatabase {
	endpoint := testingFixtureEndpoint
	return config.OwnedDatabase{Name: name, Engine: "mysql", Role: role, Endpoint: &endpoint}
}

func findAction(actions []databaseAction, name string) (databaseAction, bool) {
	for _, action := range actions {
		if action.Name == name {
			return action, true
		}
	}
	return databaseAction{}, false
}

// An owned, isolated testing database is migrated non-destructively by
// default, whether this run created it or it pre-existed: preparation runs on
// every ensure, possibly while tests use the database.
func TestTestingDatabaseDefaultPlanMigratesOwnedIsolatedTarget(t *testing.T) {
	for _, tc := range []struct{ created, action string }{{"true", "migrate"}, {"false", "migrate"}} {
		t.Run("created="+tc.created, func(t *testing.T) {
			root := t.TempDir()
			writeTestingFixture(t, root, ownedFixtureDatabase(testingFixtureApp, config.DbRoleApplication), ownedFixtureDatabase(testingFixtureTest, config.DbRoleTesting))
			actions, err := planDatabaseActions(testingFixtureContext(root, tc.created))
			require.NoError(t, err)
			app, ok := findAction(actions, testingFixtureApp)
			require.True(t, ok)
			require.Equal(t, "skip", app.Action, "an existing application database is never migrated by default")
			testing, ok := findAction(actions, testingFixtureTest)
			require.True(t, ok, "the owned testing database must be targeted")
			require.Equal(t, tc.action, testing.Action)
			require.Equal(t, config.DbRoleTesting, testing.Role)
			require.False(t, testing.Seed, "testing databases are never seeded by default")
		})
	}
}

// Only a recorded, verified, non-borrowed testing database whose name is this
// run's TestDatabaseName for the worktree suffix is targeted by default.
func TestTestingDatabaseDefaultPlanNeverTargetsUnrelatedDatabases(t *testing.T) {
	legacy := config.OwnedDatabase{Name: testingFixtureTest, Engine: "mysql", Role: config.DbRoleTesting}
	for _, tc := range []struct {
		name      string
		databases []config.OwnedDatabase
		mutate    func(*types.ScaffoldContext)
		created   string
	}{
		{name: "unrecorded same name", databases: []config.OwnedDatabase{ownedFixtureDatabase(testingFixtureApp, config.DbRoleApplication)}, created: "false"},
		{name: "auxiliary record", databases: []config.OwnedDatabase{ownedFixtureDatabase(testingFixtureApp, config.DbRoleApplication), ownedFixtureDatabase("demo_other_test", config.DbRoleTesting), ownedFixtureDatabase(testingFixtureTest, config.DbRoleAuxiliary)}, created: "false"},
		{name: "testing record for another name", databases: []config.OwnedDatabase{ownedFixtureDatabase("demo_old_name_test", config.DbRoleTesting)}, created: "false"},
		{name: "name outside worktree suffix", databases: []config.OwnedDatabase{ownedFixtureDatabase(testingFixtureTest, config.DbRoleTesting)}, created: "false", mutate: func(ctx *types.ScaffoldContext) { ctx.SetDbSuffix("other_suffix") }},
		{name: "unverified legacy endpoint", databases: []config.OwnedDatabase{legacy}, created: "false"},
		{name: "unverified legacy endpoint created", databases: []config.OwnedDatabase{legacy}, created: "true"},
		{name: "testing db.create did not run", databases: []config.OwnedDatabase{ownedFixtureDatabase(testingFixtureTest, config.DbRoleTesting)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeTestingFixture(t, root, tc.databases...)
			ctx := testingFixtureContext(root, tc.created)
			if tc.mutate != nil {
				tc.mutate(ctx)
			}
			actions, err := planDatabaseActions(ctx)
			require.NoError(t, err)
			for _, action := range actions {
				require.NotEqual(t, config.DbRoleTesting, action.Role, "unexpected testing target %+v", action)
				require.NotEqual(t, testingFixtureTest, action.Name, "unexpected target %+v", action)
				require.NotEqual(t, "fresh", action.Action)
			}
		})
	}
}

func TestTestingDatabaseDefaultPlanHonoursOverridesAndSharedWorkflows(t *testing.T) {
	t.Run("user skip wins", func(t *testing.T) {
		root := t.TempDir()
		writeTestingFixture(t, root, ownedFixtureDatabase(testingFixtureApp, config.DbRoleApplication), ownedFixtureDatabase(testingFixtureTest, config.DbRoleTesting))
		ctx := testingFixtureContext(root, "false")
		ctx.DatabaseActions = map[string]string{"testing": "skip"}
		actions, err := planDatabaseActions(ctx)
		require.NoError(t, err)
		testing, ok := findAction(actions, testingFixtureTest)
		require.True(t, ok)
		require.Equal(t, "skip", testing.Action)
	})
	t.Run("user migrate stays non-destructive", func(t *testing.T) {
		root := t.TempDir()
		writeTestingFixture(t, root, ownedFixtureDatabase(testingFixtureTest, config.DbRoleTesting))
		ctx := testingFixtureContext(root, "false")
		ctx.DatabaseActions = map[string]string{"testing": "migrate"}
		actions, err := planDatabaseActions(ctx)
		require.NoError(t, err)
		testing, ok := findAction(actions, testingFixtureTest)
		require.True(t, ok)
		require.Equal(t, "migrate", testing.Action)
	})
	t.Run("shared database preset has no testing target", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("DB_CONNECTION=mysql\nDB_DATABASE=shared_app\n"), 0o600))
		ctx := &types.ScaffoldContext{WorktreePath: root, SiteName: "demo", DbSuffix: testingFixtureSuffix}
		actions, err := planDatabaseActions(ctx)
		require.NoError(t, err)
		require.Len(t, actions, 1)
		require.Equal(t, "shared_app", actions[0].Name)
		require.Equal(t, "skip", actions[0].Action)
	})
	t.Run("borrowed application stays skipped", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("DB_CONNECTION=mysql\nDB_DATABASE=owner_app\n"), 0o600))
		borrowed := &config.BorrowedApplication{SourcePath: filepath.Join(root, "owner"), Name: "owner_app", Engine: "mysql", Driver: "yerd", Service: "mysql", Host: "127.0.0.1", Port: "3306"}
		require.NoError(t, config.WriteLocalState(root, config.LocalState{DbSuffix: testingFixtureSuffix, BorrowedApplication: borrowed, Databases: []config.OwnedDatabase{ownedFixtureDatabase(testingFixtureTest, config.DbRoleTesting)}}))
		ctx := testingFixtureContext(root, "false")
		ctx.SetVar("DatabaseName", "owner_app")
		actions, err := planDatabaseActions(ctx)
		require.NoError(t, err)
		app, ok := findAction(actions, "owner_app")
		require.True(t, ok)
		require.True(t, app.Borrowed)
		require.Equal(t, "skip", app.Action, "borrowed application data is never migrated by default")
		testing, ok := findAction(actions, testingFixtureTest)
		require.True(t, ok, "a borrower's own testing database is still prepared")
		require.Equal(t, "migrate", testing.Action)
	})
	t.Run("explicit fresh for the isolated testing database", func(t *testing.T) {
		for _, selector := range []string{"testing", testingFixtureTest} {
			root := t.TempDir()
			writeTestingFixture(t, root, ownedFixtureDatabase(testingFixtureApp, config.DbRoleApplication), ownedFixtureDatabase(testingFixtureTest, config.DbRoleTesting))
			ctx := testingFixtureContext(root, "false")
			ctx.DatabaseActions = map[string]string{selector: "fresh"}
			actions, err := planDatabaseActions(ctx)
			require.NoError(t, err)
			testing, ok := findAction(actions, testingFixtureTest)
			require.True(t, ok)
			require.Equal(t, "fresh", testing.Action)
			require.False(t, testing.Seed)
		}
	})
	t.Run("reset semantics unchanged", func(t *testing.T) {
		root := t.TempDir()
		writeTestingFixture(t, root, ownedFixtureDatabase(testingFixtureTest, config.DbRoleTesting))
		ctx := testingFixtureContext(root, "false")
		ctx.ResetDatabases = []string{testingFixtureTest}
		actions, err := planDatabaseActions(ctx)
		require.NoError(t, err)
		testing, ok := findAction(actions, testingFixtureTest)
		require.True(t, ok)
		require.Equal(t, "reset", testing.Action)
	})
}

// installPolicyPHP puts a php stand-in on PATH that logs DB_DATABASE and its
// arguments, failing for the database named in ANVIL_POLICY_FAIL.
func installPolicyPHP(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "commands")
	script := "#!/bin/sh\nif [ \"$DB_DATABASE\" = \"$ANVIL_POLICY_FAIL\" ]; then echo 'SQLSTATE[42000]: syntax error' >&2; exit 3; fi\nprintf '%s|%s|%s\\n' \"$DB_DATABASE\" \"$DB_CONNECTION\" \"$*\" >> \"$ANVIL_POLICY_LOG\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(bin, "php"), []byte(script), 0o700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ANVIL_POLICY_LOG", log)
	t.Setenv("ANVIL_POLICY_FAIL", "")
	return log
}

func readPolicyLog(t *testing.T, log string) string {
	t.Helper()
	data, err := os.ReadFile(log)
	if os.IsNotExist(err) {
		return ""
	}
	require.NoError(t, err)
	return string(data)
}

// Driving the real db.create steps proves the default Laravel ordering
// (db.create, db.create role=testing, db.migrate) makes the testing database
// schema-ready on first scaffold and rebuilds it on re-scaffold without
// touching the application database or .env.
func TestTestingDatabaseIsSchemaReadyAfterCreateAndRescaffold(t *testing.T) {
	log := installPolicyPHP(t)
	root := t.TempDir()
	env := []byte("DB_CONNECTION=mysql\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), env, 0o600))
	require.NoError(t, config.WriteLocalState(root, config.LocalState{DbSuffix: testingFixtureSuffix}))
	client := NewMockDatabaseClient()
	factory := MockClientFactory(client)
	scaffold := func() {
		ctx := &types.ScaffoldContext{WorktreePath: root, SiteName: "demo", DbSuffix: testingFixtureSuffix}
		ctx.SetDbSuffixLoadedFromState()
		require.NoError(t, NewDbCreateStepWithFactory(config.StepConfig{}, factory).Run(ctx, types.StepOptions{}))
		require.NoError(t, NewDbCreateStepWithFactory(config.StepConfig{Role: config.DbRoleTesting}, factory).Run(ctx, types.StepOptions{}))
		require.NoError(t, (&DbMigrateStep{clientFactory: factory}).Run(ctx, types.StepOptions{}))
	}

	scaffold()
	require.Equal(t, testingFixtureApp+"|mysql|artisan migrate --no-interaction --seed\n"+
		testingFixtureTest+"|mysql|artisan migrate --no-interaction\n", readPolicyLog(t, log))

	require.NoError(t, os.Remove(log))
	scaffold()
	require.Equal(t, testingFixtureTest+"|mysql|artisan migrate --no-interaction\n", readPolicyLog(t, log),
		"re-scaffold migrates only the owned testing database, non-destructively and without seeding")

	// A missing owned testing database is recreated and migrated, not reset.
	require.NoError(t, os.Remove(log))
	require.NoError(t, client.DropDatabase(testingFixtureTest))
	scaffold()
	require.True(t, client.HasDatabase(testingFixtureTest))
	require.Equal(t, testingFixtureTest+"|mysql|artisan migrate --no-interaction\n", readPolicyLog(t, log))

	after, err := os.ReadFile(filepath.Join(root, ".env"))
	require.NoError(t, err)
	require.Equal(t, env, after, "migrations never edit .env")
}

func TestTestingDatabaseMigrationFailureNamesDatabaseRoleAndAction(t *testing.T) {
	log := installPolicyPHP(t)
	root := t.TempDir()
	writeTestingFixture(t, root, ownedFixtureDatabase(testingFixtureApp, config.DbRoleApplication), ownedFixtureDatabase(testingFixtureTest, config.DbRoleTesting))
	t.Setenv("ANVIL_POLICY_FAIL", testingFixtureTest)
	client := NewMockDatabaseClient()
	client.AddDatabase(testingFixtureApp)
	client.AddDatabase(testingFixtureTest)
	err := (&DbMigrateStep{clientFactory: MockClientFactory(client)}).Run(testingFixtureContext(root, "false"), types.StepOptions{})
	require.Error(t, err)
	firstLine, _, _ := strings.Cut(err.Error(), "\n")
	require.Contains(t, firstLine, testingFixtureTest)
	require.Contains(t, firstLine, "role=testing")
	require.Contains(t, firstLine, "action=migrate")
	require.Contains(t, firstLine, "rerun 'anvil scaffold'")
	require.Contains(t, firstLine, "database_actions: {testing: skip}")
	require.Contains(t, err.Error(), "SQLSTATE[42000]")
	require.Empty(t, readPolicyLog(t, log))
}

// Dry-run prints the planned testing action and runs nothing; a fresh
// worktree's testing database is planned as migrate because db.create would
// create and record it.
func TestTestingDatabaseDryRunPrintsPlannedAction(t *testing.T) {
	log := installPolicyPHP(t)
	for _, tc := range []struct {
		name     string
		existing bool
		want     string
		fresh    bool
	}{{"fresh worktree", false, "action=migrate", false}, {"existing owned", true, "action=migrate", false}, {"existing owned opt-in", true, "action=fresh", true}} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("DB_CONNECTION=mysql\n"), 0o600))
			client := NewMockDatabaseClient()
			if tc.existing {
				writeTestingFixture(t, root, ownedFixtureDatabase(testingFixtureApp, config.DbRoleApplication), ownedFixtureDatabase(testingFixtureTest, config.DbRoleTesting))
				client.AddDatabase(testingFixtureApp)
				client.AddDatabase(testingFixtureTest)
			}
			factory := MockClientFactory(client)
			ctx := &types.ScaffoldContext{WorktreePath: root, SiteName: "demo", DbSuffix: testingFixtureSuffix}
			if tc.fresh {
				ctx.DatabaseActions = map[string]string{"testing": "fresh"}
			}
			output := captureStdout(t, func() {
				require.NoError(t, NewDbCreateStepWithFactory(config.StepConfig{}, factory).Run(ctx, types.StepOptions{DryRun: true}))
				require.NoError(t, NewDbCreateStepWithFactory(config.StepConfig{Role: config.DbRoleTesting}, factory).Run(ctx, types.StepOptions{DryRun: true}))
				require.NoError(t, (&DbMigrateStep{clientFactory: factory}).Run(ctx, types.StepOptions{DryRun: true}))
			})
			var line string
			for _, candidate := range strings.Split(output, "\n") {
				if strings.HasPrefix(candidate, "Database "+testingFixtureTest+" ") {
					line = candidate
				}
			}
			require.NotEmpty(t, line, "dry-run must print the testing plan:\n%s", output)
			require.Contains(t, line, "role=testing")
			require.Contains(t, line, tc.want)
			require.Contains(t, line, "seed=false")
			require.Empty(t, readPolicyLog(t, log))
			require.Empty(t, client.GetCreateCalls())
		})
	}
}

// A planned (dry-run only) unrecorded testing target can never authorize a
// real command.
func TestUnrecordedTestingTargetNeverRunsOutsideDryRun(t *testing.T) {
	log := installPolicyPHP(t)
	root := t.TempDir()
	writeTestingFixture(t, root, ownedFixtureDatabase(testingFixtureApp, config.DbRoleApplication))
	client := NewMockDatabaseClient()
	client.AddDatabase(testingFixtureApp)
	client.AddDatabase(testingFixtureTest)
	err := (&DbMigrateStep{clientFactory: MockClientFactory(client)}).Run(testingFixtureContext(root, "true"), types.StepOptions{})
	require.ErrorContains(t, err, "not recorded as owned")
	require.Empty(t, readPolicyLog(t, log))
}

// Opt-in migrate:fresh is refused when any other registered worktree records
// or borrows the same database name, even though this worktree owns it.
func TestTestingDatabaseFreshRefusesNameReferencedBySibling(t *testing.T) {
	log := installPolicyPHP(t)
	base := t.TempDir()
	owner := filepath.Join(base, "owner")
	out, err := exec.Command("git", "init", owner).CombinedOutput()
	require.NoError(t, err, string(out))
	sibling := filepath.Join(base, "sibling")
	out, err = exec.Command("git", "-C", owner, "worktree", "add", "--orphan", sibling).CombinedOutput()
	require.NoError(t, err, string(out))
	writeTestingFixture(t, owner, ownedFixtureDatabase(testingFixtureTest, config.DbRoleTesting))
	writeTestingFixture(t, sibling, ownedFixtureDatabase(testingFixtureTest, config.DbRoleTesting))
	client := NewMockDatabaseClient()
	client.AddDatabase(testingFixtureTest)
	freshContext := func() *types.ScaffoldContext {
		ctx := testingFixtureContext(owner, "false")
		ctx.DatabaseActions = map[string]string{"testing": "fresh"}
		return ctx
	}
	err = (&DbMigrateStep{clientFactory: MockClientFactory(client)}).Run(freshContext(), types.StepOptions{})
	require.ErrorContains(t, err, "also recorded by worktree")
	require.ErrorContains(t, err, testingFixtureTest)
	require.ErrorContains(t, err, "database_actions: {testing: migrate}")
	require.Empty(t, readPolicyLog(t, log))

	// Once the sibling no longer references it, the rebuild proceeds.
	require.NoError(t, os.Remove(filepath.Join(sibling, config.LocalStateFile)))
	require.NoError(t, (&DbMigrateStep{clientFactory: MockClientFactory(client)}).Run(freshContext(), types.StepOptions{}))
	require.Equal(t, testingFixtureTest+"|mysql|artisan migrate:fresh --no-interaction\n", readPolicyLog(t, log))
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	original := os.Stdout
	os.Stdout = writer
	done := make(chan []byte)
	go func() {
		var buffer bytes.Buffer
		_, copyErr := io.Copy(&buffer, reader)
		if copyErr != nil {
			buffer.WriteString("copy error: " + copyErr.Error())
		}
		done <- buffer.Bytes()
	}()
	defer func() { os.Stdout = original }()
	fn()
	require.NoError(t, writer.Close())
	output := <-done
	require.NoError(t, reader.Close())
	return string(output)
}

// The documented pre-1.8 remedy (an override with db.create role=testing,
// optionally followed by db.migrate) prepares only the new testing database
// and never touches the unrecorded application database.
func TestLegacyTestingOverrideMigratesOnlyTestingDatabase(t *testing.T) {
	log := installPolicyPHP(t)
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("DB_CONNECTION=mysql\nDB_DATABASE="+testingFixtureApp+"\n"), 0o600))
	require.NoError(t, config.WriteLocalState(root, config.LocalState{DbSuffix: testingFixtureSuffix}))
	client := NewMockDatabaseClient()
	client.AddDatabase(testingFixtureApp)
	factory := MockClientFactory(client)
	ctx := &types.ScaffoldContext{WorktreePath: root, SiteName: "demo", DbSuffix: testingFixtureSuffix}
	ctx.SetDbSuffixLoadedFromState()
	require.NoError(t, NewDbCreateStepWithFactory(config.StepConfig{Role: config.DbRoleTesting}, factory).Run(ctx, types.StepOptions{}))
	require.NoError(t, (&DbMigrateStep{clientFactory: factory}).Run(ctx, types.StepOptions{}))
	require.Equal(t, testingFixtureTest+"|mysql|artisan migrate --no-interaction\n", readPolicyLog(t, log))
}

// fresh is accepted only for the canonical owned, isolated testing database.
func TestFreshActionRejectedOutsideIsolatedTestingDatabase(t *testing.T) {
	legacy := config.OwnedDatabase{Name: testingFixtureTest, Engine: "mysql", Role: config.DbRoleTesting}
	for _, tc := range []struct {
		name      string
		databases []config.OwnedDatabase
		selector  string
		created   string
	}{
		{name: "application selector", databases: []config.OwnedDatabase{ownedFixtureDatabase(testingFixtureApp, config.DbRoleApplication), ownedFixtureDatabase(testingFixtureTest, config.DbRoleTesting)}, selector: "application", created: "false"},
		{name: "exact application name", databases: []config.OwnedDatabase{ownedFixtureDatabase(testingFixtureApp, config.DbRoleApplication), ownedFixtureDatabase(testingFixtureTest, config.DbRoleTesting)}, selector: testingFixtureApp, created: "false"},
		{name: "auxiliary record", databases: []config.OwnedDatabase{ownedFixtureDatabase("demo_other_test", config.DbRoleTesting), ownedFixtureDatabase(testingFixtureTest, config.DbRoleAuxiliary)}, selector: testingFixtureTest, created: "false"},
		{name: "testing record for another name", databases: []config.OwnedDatabase{ownedFixtureDatabase("demo_old_name_test", config.DbRoleTesting), ownedFixtureDatabase(testingFixtureTest, config.DbRoleAuxiliary)}, selector: "testing", created: "false"},
		{name: "unverified legacy endpoint", databases: []config.OwnedDatabase{legacy}, selector: "testing", created: "false"},
		{name: "testing db.create did not run", databases: []config.OwnedDatabase{ownedFixtureDatabase(testingFixtureTest, config.DbRoleTesting)}, selector: "testing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeTestingFixture(t, root, tc.databases...)
			ctx := testingFixtureContext(root, tc.created)
			ctx.DatabaseActions = map[string]string{tc.selector: "fresh"}
			_, err := planDatabaseActions(ctx)
			require.ErrorContains(t, err, "fresh is only allowed for this worktree's owned, isolated testing database")
		})
	}
}

// The repository database lock is held across the isolation check and the
// migrate:fresh command, and released afterwards.
func TestFreshHoldsDatabaseLockAcrossCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	if _, err := exec.LookPath("flock"); err != nil {
		t.Skip("flock(1) is required to probe the lock from the command")
	}
	root := t.TempDir()
	out, err := exec.Command("git", "init", root).CombinedOutput()
	require.NoError(t, err, string(out))
	writeTestingFixture(t, root, ownedFixtureDatabase(testingFixtureTest, config.DbRoleTesting))
	bin := t.TempDir()
	probe := filepath.Join(t.TempDir(), "probe")
	script := "#!/bin/sh\nif flock -n \"$ANVIL_POLICY_LOCK\" true; then echo unlocked > \"$ANVIL_POLICY_PROBE\"; else echo locked > \"$ANVIL_POLICY_PROBE\"; fi\n"
	require.NoError(t, os.WriteFile(filepath.Join(bin, "php"), []byte(script), 0o700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ANVIL_POLICY_LOCK", filepath.Join(root, ".git", "anvil-database-lifecycle.lock"))
	t.Setenv("ANVIL_POLICY_PROBE", probe)
	client := NewMockDatabaseClient()
	client.AddDatabase(testingFixtureTest)
	ctx := testingFixtureContext(root, "false")
	ctx.DatabaseActions = map[string]string{"testing": "fresh"}
	require.NoError(t, (&DbMigrateStep{clientFactory: MockClientFactory(client)}).Run(ctx, types.StepOptions{}))
	data, err := os.ReadFile(probe)
	require.NoError(t, err)
	require.Equal(t, "locked\n", string(data), "migrate:fresh must run under the database lifecycle lock")
	require.False(t, ctx.DatabaseLockHeld)
	release, err := dblifecycle.Lock(root)
	require.NoError(t, err, "the lock is released after the step")
	require.NoError(t, release())
}
