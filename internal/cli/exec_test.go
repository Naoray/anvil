package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
)

// writeExecStateFile writes a raw .anvil.local so tests can construct states
// (e.g. duplicate roles) that config.WriteLocalState's canonical merge would
// never produce.
func writeExecStateFile(t *testing.T, dir, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.LocalStateFile), []byte(content), 0o644))
}

func countEnvKey(env []string, key string) int {
	count := 0
	for _, entry := range env {
		if strings.HasPrefix(entry, key+"=") {
			count++
		}
	}
	return count
}

func TestBuildExecEnv_ReplacesInheritedValues(t *testing.T) {
	env := buildExecEnv([]string{
		"DB_DATABASE=inherited_db",
		"ANVIL_DB_DATABASE=inherited_app",
		"ANVIL_TEST_DB_DATABASE=inherited_test",
		"PATH=/usr/bin",
	}, "demo_app", "demo_app_test", false)

	assert.Contains(t, env, "DB_DATABASE=demo_app_test")
	assert.Contains(t, env, "ANVIL_TEST_DB_DATABASE=demo_app_test")
	assert.Contains(t, env, "ANVIL_DB_DATABASE=demo_app")
	assert.Contains(t, env, "PATH=/usr/bin")
	assert.Equal(t, 1, countEnvKey(env, "DB_DATABASE"))
	assert.Equal(t, 1, countEnvKey(env, "ANVIL_TEST_DB_DATABASE"))
	assert.Equal(t, 1, countEnvKey(env, "ANVIL_DB_DATABASE"))
	assert.NotContains(t, env, "DB_DATABASE=inherited_db")
	assert.NotContains(t, env, "ANVIL_DB_DATABASE=inherited_app")
	assert.NotContains(t, env, "ANVIL_TEST_DB_DATABASE=inherited_test")
}

func TestBuildExecEnv_OmitsUnknownApplicationDatabase(t *testing.T) {
	env := buildExecEnv([]string{"ANVIL_DB_DATABASE=stale"}, "", "demo_test", false)

	assert.Contains(t, env, "DB_DATABASE=demo_test")
	assert.Contains(t, env, "ANVIL_TEST_DB_DATABASE=demo_test")
	assert.Equal(t, 0, countEnvKey(env, "ANVIL_DB_DATABASE"))
}

func TestBuildExecEnv_WindowsCaseInsensitiveKeys(t *testing.T) {
	out := buildExecEnv([]string{"db_database=stale", "Db_DaTaBaSe=stale2", "PATH=/bin"}, "app", "test", true)

	for _, entry := range out {
		key, _, found := strings.Cut(entry, "=")
		require.True(t, found, "malformed env entry %q", entry)
		if strings.EqualFold(key, "DB_DATABASE") {
			assert.Equal(t, "DB_DATABASE=test", entry, "case-variant DB_DATABASE survived: %q", entry)
		}
	}
	assert.Contains(t, out, "DB_DATABASE=test")
	assert.Contains(t, out, "PATH=/bin")
	assert.NotContains(t, out, "db_database=stale")
	assert.NotContains(t, out, "Db_DaTaBaSe=stale2")
}

func TestFindWorktreeRoot(t *testing.T) {
	root := t.TempDir()
	writeExecStateFile(t, root, "db_suffix: top_provider\n")
	nested := filepath.Join(root, "app", "Http")
	require.NoError(t, os.MkdirAll(nested, 0o755))

	found, err := findWorktreeRoot(nested)
	require.NoError(t, err)
	assert.Equal(t, root, found)

	empty := t.TempDir()
	_, err = findWorktreeRoot(empty)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no .anvil.local found")
	assert.Contains(t, err.Error(), "anvil scaffold")
	assert.Contains(t, err.Error(), empty)
}

func TestResolveExecDatabases_FromOwnedState(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, config.WriteLocalState(dir, config.LocalState{
		DbSuffix: "top_provider",
		Databases: []config.OwnedDatabase{
			{Name: "demo_top_provider", Engine: "mysql", Role: config.DbRoleApplication},
			{Name: "demo_top_provider_test", Engine: "mysql", Role: config.DbRoleTesting},
		},
	}))

	appDb, testDb, err := resolveExecDatabases(dir)
	require.NoError(t, err)
	assert.Equal(t, "demo_top_provider", appDb)
	assert.Equal(t, "demo_top_provider_test", testDb)
}

func TestResolveExecDatabases_TestingOnlyStateOmitsAppDb(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, config.WriteLocalState(dir, config.LocalState{
		DbSuffix: "top_provider",
		Databases: []config.OwnedDatabase{
			{Name: "demo_top_provider_test", Engine: "pgsql", Role: config.DbRoleTesting},
		},
	}))

	appDb, testDb, err := resolveExecDatabases(dir)
	require.NoError(t, err)
	assert.Empty(t, appDb)
	assert.Equal(t, "demo_top_provider_test", testDb)
}

func TestResolveExecDatabases_DuplicateTestingRecordsRefused(t *testing.T) {
	dir := t.TempDir()
	writeExecStateFile(t, dir, `db_suffix: top_provider
databases:
  - name: demo_a_test
    engine: mysql
    role: testing
  - name: demo_b_test
    engine: mysql
    role: testing
`)

	_, _, err := resolveExecDatabases(dir)
	require.Error(t, err)
	assert.EqualError(t, err, `duplicate database role "testing" in record 1; first seen in record 0`)
}

func TestResolveExecDatabases_DuplicateApplicationRecordsRefused(t *testing.T) {
	dir := t.TempDir()
	writeExecStateFile(t, dir, `db_suffix: top_provider
databases:
  - name: demo_one
    engine: mysql
    role: application
  - name: demo_two
    engine: mysql
    role: application
  - name: demo_one_test
    engine: mysql
    role: testing
`)

	_, _, err := resolveExecDatabases(dir)
	require.Error(t, err)
	assert.EqualError(t, err, `duplicate database role "application" in record 1; first seen in record 0`)
}

func TestResolveExecDatabases_DuplicateDatabaseNamesRefused(t *testing.T) {
	dir := t.TempDir()
	writeExecStateFile(t, dir, `db_suffix: top_provider
databases:
  - name: demo_database
    engine: mysql
    role: application
  - name: demo_database
    engine: mysql
    role: testing
`)

	_, _, err := resolveExecDatabases(dir)
	require.Error(t, err)
	assert.EqualError(t, err, `duplicate database name "demo_database" in record 1; first seen in record 0`)
}

func TestResolveExecDatabases_UnsupportedRoleRefused(t *testing.T) {
	dir := t.TempDir()
	writeExecStateFile(t, dir, `db_suffix: top_provider
databases:
  - name: demo_top_provider
    engine: mysql
    role: someday-role
  - name: demo_top_provider_test
    engine: mysql
    role: testing
`)

	_, _, err := resolveExecDatabases(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unsupported database record role "someday-role"`)
	assert.Contains(t, err.Error(), "demo_top_provider")
}

func TestResolveExecDatabases_LegacyStateFailSafeReadOnly(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, config.WriteLocalState(dir, config.LocalState{DbSuffix: "top_provider"}))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"),
		[]byte("DB_CONNECTION=mysql\nDB_DATABASE=demo_top_provider\n"), 0o644))
	before, err := os.ReadFile(filepath.Join(dir, config.LocalStateFile))
	require.NoError(t, err)

	_, _, err = resolveExecDatabases(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "v1.8")
	assert.Contains(t, err.Error(), "migrate:fresh")
	assert.NotContains(t, err.Error(), "shared database")

	after, err := os.ReadFile(filepath.Join(dir, config.LocalStateFile))
	require.NoError(t, err)
	assert.Equal(t, before, after, "resolveExecDatabases must never write .anvil.local")
}

func TestResolveExecDatabases_SharedDbMessage(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, config.WriteLocalState(dir, config.LocalState{DbSuffix: "top_provider"}))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"),
		[]byte("DB_CONNECTION=mysql\nDB_DATABASE=demo\n"), 0o644))

	_, _, err := resolveExecDatabases(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shared database")
	assert.Contains(t, err.Error(), "v1.8")
}

func TestResolveExecDatabases_SqliteMessage(t *testing.T) {
	t.Run("env connection sqlite", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, config.WriteLocalState(dir, config.LocalState{DbSuffix: "top_provider"}))
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"),
			[]byte("DB_CONNECTION=sqlite\n"), 0o644))

		_, _, err := resolveExecDatabases(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "SQLite")
		assert.Contains(t, err.Error(), "already isolated")
		assert.NotContains(t, err.Error(), "v1.8")
	})

	t.Run("recorded sqlite engine beats generic validator", func(t *testing.T) {
		dir := t.TempDir()
		writeExecStateFile(t, dir, `db_suffix: top_provider
databases:
  - name: demo_top_provider
    engine: sqlite
    role: application
`)

		_, _, err := resolveExecDatabases(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "SQLite")
		assert.NotContains(t, err.Error(), "unsupported database engine")
	})
}

func TestResolveExecDatabases_MultipleEnginesRefused(t *testing.T) {
	dir := t.TempDir()
	writeExecStateFile(t, dir, `db_suffix: top_provider
databases:
  - name: demo_top_provider
    engine: mysql
    role: application
  - name: demo_top_provider_test
    engine: pgsql
    role: testing
`)

	_, _, err := resolveExecDatabases(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one supported engine")
}

func TestResolveExecDatabases_InvalidRecordedNameRefused(t *testing.T) {
	dir := t.TempDir()
	writeExecStateFile(t, dir, `db_suffix: top_provider
databases:
  - name: bad;name
    engine: mysql
    role: testing
`)

	_, _, err := resolveExecDatabases(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid database name")
}

func TestChildExitError(t *testing.T) {
	withMessage := &ChildExitError{Code: 127, Message: "command not found: nope"}
	assert.Equal(t, "command not found: nope", withMessage.Error())

	wrapped := fmt.Errorf("running child: %w", withMessage)
	var childErr *ChildExitError
	require.True(t, errors.As(wrapped, &childErr))
	assert.Equal(t, 127, childErr.Code)
	assert.Equal(t, "command not found: nope", childErr.Message)

	bare := &ChildExitError{Code: 7}
	assert.NotEmpty(t, bare.Error())
}
