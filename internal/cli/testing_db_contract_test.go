package cli

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
)

// fakeYerdScript emulates the Yerd commands db.create/db.migrate use: one
// running MySQL service on 127.0.0.1:3306, and one file per database in
// $FAKE_YERD_STATE. Site commands (link, secure, unlink) succeed.
const fakeYerdScript = `#!/bin/sh
state="$FAKE_YERD_STATE"
[ "$1" = --json ] && shift
case "$1" in
services)
  printf '{"services":[{"service":"mysql","state":"running","listen":"127.0.0.1:3306","port":3306,"installed_versions":["8.0"]}]}\n'
  exit 0;;
db)
  case "$2" in
  list)
    printf '{"databases":['
    sep=
    for f in "$state"/*; do
      [ -e "$f" ] || continue
      printf '%s{"name":"%s"}' "$sep" "$(basename "$f")"
      sep=,
    done
    printf ']}\n'
    exit 0;;
  create)
    if [ -e "$state/$4" ]; then echo "database $4 already exists"; exit 1; fi
    : > "$state/$4"
    exit 0;;
  drop)
    rm -f "$state/$4"
    exit 0;;
  esac;;
esac
exit 0
`

// fakePHPScript emulates artisan migrations against the fake Yerd state: the
// database must exist (as Laravel would fail on an unknown database), migrate
// appends "schema" (and "seed" with --seed), migrate:fresh wipes the database
// first. Every migration is logged per database with its DB_CONNECTION.
// Databases whose name ends in $FAKE_PHP_FAIL_SUFFIX fail to migrate.
const fakePHPScript = `#!/bin/sh
if [ "$1" = artisan ]; then
  case "$2" in
  migrate|migrate:fresh)
    db="$FAKE_YERD_STATE/$DB_DATABASE"
    if [ -z "$DB_DATABASE" ] || [ ! -f "$db" ]; then
      echo "SQLSTATE[HY000] [1049] Unknown database '$DB_DATABASE'" >&2
      exit 1
    fi
    if [ -n "$FAKE_PHP_FAIL_SUFFIX" ]; then
      case "$DB_DATABASE" in
      *"$FAKE_PHP_FAIL_SUFFIX") echo "SQLSTATE[42S01]: Base table or view already exists" >&2; exit 1;;
      esac
    fi
    shift
    echo "$DB_CONNECTION $*" >> "$FAKE_SCHEMA_LOG/$DB_DATABASE"
    if [ "$1" = migrate:fresh ]; then : > "$db"; fi
    echo schema >> "$db"
    for arg in "$@"; do
      if [ "$arg" = --seed ]; then echo seed >> "$db"; fi
    done
    exit 0;;
  esac
fi
exit 0
`

type testingDBFixture struct {
	*agentFixture
	yerdState string
	schemaLog string
}

func newTestingDBFixture(t *testing.T) *testingDBFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX shell stand-ins for php and yerd")
	}
	files := map[string]string{
		"composer.json": "{}\n",
		"artisan":       "<?php\n",
		".env.example":  "APP_NAME=demo\nAPP_KEY=base64:fixture\nDB_CONNECTION=mysql\nDB_HOST=127.0.0.1\nDB_PORT=3306\nDB_USERNAME=root\nDB_PASSWORD=fixture-secret-value\n",
		".gitignore":    ".anvil.local\n.env\n*.count\n",
	}
	f := newAgentFixture(t, "preset: laravel\n", files)
	fakeBin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(fakeBin, "yerd"), []byte(fakeYerdScript), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fakeBin, "php"), []byte(fakePHPScript), 0o755))
	fixture := &testingDBFixture{agentFixture: f, yerdState: t.TempDir(), schemaLog: t.TempDir()}
	f.env = subprocessEnvFrom(append(f.env,
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_YERD_STATE="+fixture.yerdState,
		"FAKE_SCHEMA_LOG="+fixture.schemaLog,
	))
	return fixture
}

func (f *testingDBFixture) databases(t *testing.T, worktree string) (app, testing string) {
	t.Helper()
	state, err := config.ReadLocalState(worktree)
	require.NoError(t, err)
	for _, database := range state.Databases {
		switch database.Role {
		case config.DbRoleApplication:
			app = database.Name
		case config.DbRoleTesting:
			testing = database.Name
		}
	}
	require.NotEmpty(t, app, "application database must be recorded")
	require.NotEmpty(t, testing, "testing database must be recorded")
	return app, testing
}

func (f *testingDBFixture) contents(t *testing.T, database string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.yerdState, database))
	require.NoError(t, err, "database %s must exist", database)
	return string(data)
}

func (f *testingDBFixture) migrations(t *testing.T, database string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.schemaLog, database))
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

// execEnv runs `anvil exec` with the helper child and returns the managed
// variables it observed.
func (f *testingDBFixture) execEnv(t *testing.T, worktree string) map[string]string {
	t.Helper()
	saved := f.env
	f.env = subprocessEnvFrom(append(append([]string(nil), saved...), "ANVIL_TEST_HELPER=1", "ANVIL_HELPER_MODE=env"))
	defer func() { f.env = saved }()
	stdout, stderr, code := f.run(worktree, "exec", helperSelf(t))
	require.Equal(t, 0, code, stdout+stderr)
	values := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(stdout))
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if ok {
			values[key] = value
		}
	}
	require.NoError(t, scanner.Err())
	return values
}

// The full contract: anvil work provisions and migrates the owned testing
// database, anvil exec exports exactly that database, re-preparing migrates
// it non-destructively without touching application data, migrate:fresh (no
// seed) runs only on explicit opt-in, and a testing database missing on the
// server is recreated and migrated.
func TestScaffoldLeavesExportedTestingDatabaseSchemaReady(t *testing.T) {
	f := newTestingDBFixture(t)
	stdout, stderr, code := f.run(f.repo, "work", "feature/schema")
	require.Equal(t, 0, code, stdout+stderr)
	wt := f.worktreePath("feature-schema")
	app, testing := f.databases(t, wt)

	exported := f.execEnv(t, wt)
	require.Equal(t, testing, exported["DB_DATABASE"])
	require.Equal(t, testing, exported["ANVIL_TEST_DB_DATABASE"])
	require.Equal(t, app, exported["ANVIL_DB_DATABASE"])
	require.Equal(t, "schema\n", f.contents(t, exported["DB_DATABASE"]), "the exported testing database is migrated and unseeded")
	require.Equal(t, []string{"mysql migrate --no-interaction"}, f.migrations(t, testing))
	require.Equal(t, "schema\nseed\n", f.contents(t, app), "a new application database is migrated and seeded")

	// Re-preparing (ensure always runs db.migrate) never wipes the testing
	// database, so it is safe while tests use it.
	envelope, stderr, code := f.runJSON(f.repo, "ensure", "feature-schema")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, true, field(t, envelope, "data", "prepare", "ready"))
	require.Equal(t, []string{"mysql migrate --no-interaction", "mysql migrate --no-interaction"}, f.migrations(t, testing))
	require.Equal(t, "schema\nschema\n", f.contents(t, testing), "a default re-prepare keeps testing data")
	require.Equal(t, []string{"mysql migrate --no-interaction --seed"}, f.migrations(t, app), "existing application database is never migrated again")
	require.Equal(t, "schema\nseed\n", f.contents(t, app), "application data survives re-preparation")

	// migrate:fresh is explicit opt-in, and only for the testing database.
	stdout, stderr, code = f.run(f.repo, "scaffold", filepath.Base(wt), "--db-action", "application=fresh")
	require.NotEqual(t, 0, code, stdout+stderr)
	require.Contains(t, stdout+stderr, "fresh is only allowed for this worktree's owned, isolated testing database")
	require.Equal(t, "schema\nseed\n", f.contents(t, app))
	stdout, stderr, code = f.run(f.repo, "scaffold", filepath.Base(wt), "--db-action", "testing=fresh")
	require.Equal(t, 0, code, stdout+stderr)
	require.Equal(t, "mysql migrate:fresh --no-interaction", f.migrations(t, testing)[2])
	require.Equal(t, "schema\n", f.contents(t, testing), "opt-in fresh rebuilds the testing database without seeding")
	require.Equal(t, "schema\nseed\n", f.contents(t, app), "application data survives a testing rebuild")

	// exec never contacts the server: a dropped testing database is still
	// exported, and anvil scaffold is the corrective command.
	require.NoError(t, os.Remove(filepath.Join(f.yerdState, testing)))
	require.Equal(t, testing, f.execEnv(t, wt)["DB_DATABASE"])
	stdout, stderr, code = f.run(f.repo, "scaffold", filepath.Base(wt))
	require.Equal(t, 0, code, stdout+stderr)
	require.Equal(t, "schema\n", f.contents(t, testing), "missing owned testing database is recreated and migrated")
	require.Equal(t, "mysql migrate --no-interaction", f.migrations(t, testing)[3])
	require.Equal(t, "schema\nseed\n", f.contents(t, app))
	require.Equal(t, testing, f.execEnv(t, wt)["DB_DATABASE"])

	status, stderr, code := f.runJSON(f.repo, "status", "feature-schema")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, true, field(t, status, "data", "prepare", "ready"))
}

func TestScaffoldDryRunPrintsPlannedTestingMigration(t *testing.T) {
	f := newTestingDBFixture(t)
	stdout, stderr, code := f.run(f.repo, "work", "feature/plan")
	require.Equal(t, 0, code, stdout+stderr)
	wt := f.worktreePath("feature-plan")
	_, testing := f.databases(t, wt)
	before := f.migrations(t, testing)

	stdout, stderr, code = f.run(f.repo, "scaffold", filepath.Base(wt), "--dry-run")
	require.Equal(t, 0, code, stdout+stderr)
	require.Contains(t, stdout, "Database "+testing+" role=testing")
	require.Contains(t, stdout, "action=migrate")

	stdout, stderr, code = f.run(f.repo, "scaffold", filepath.Base(wt), "--dry-run", "--db-action", "testing=fresh")
	require.Equal(t, 0, code, stdout+stderr)
	require.Contains(t, stdout, "action=fresh")
	require.Equal(t, before, f.migrations(t, testing), "dry-run runs no migration")
}

func TestScaffoldTestingMigrationFailureIsNotReady(t *testing.T) {
	f := newTestingDBFixture(t)
	f.env = subprocessEnvFrom(append(f.env, "FAKE_PHP_FAIL_SUFFIX=_test"))
	stdout, stderr, code := f.run(f.repo, "work", "feature/broken-schema")
	output := stdout + stderr
	require.NotEqual(t, 0, code, output)
	require.NotContains(t, output, "Worktree ready")
	wt := f.worktreePath("feature-broken-schema")
	_, testing := f.databases(t, wt)
	require.Contains(t, output, `database "`+testing+`" role=testing action=migrate failed`)
	require.Contains(t, output, "fix the cause and rerun 'anvil scaffold' (or set database_actions: {testing: skip} to opt out)")
	require.Contains(t, output, "SQLSTATE[42S01]")
	require.NotContains(t, output, "fixture-secret-value", "credentials never appear in failures")
	requireFailedPreparation(t, f.agentFixture, "feature-broken-schema")

	// The corrective command succeeds once the migration does.
	f.env = subprocessEnvFrom(append(f.env, "FAKE_PHP_FAIL_SUFFIX="))
	stdout, stderr, code = f.run(f.repo, "scaffold", filepath.Base(wt))
	require.Equal(t, 0, code, stdout+stderr)
	require.Equal(t, "schema\n", f.contents(t, testing))
}
