package steps

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/dblifecycle"
	"github.com/naoray/anvil/internal/scaffold/types"
)

// The opt-in fixture points only at a privately initialized cluster. It never
// invokes a managed service; SQL commands use the production Herd adapter.
type privatePostgresCommander struct{}

func (privatePostgresCommander) Run(ctx context.Context, dir, command string, args ...string) ([]byte, error) {
	if command == "herd" {
		return nil, nil
	}
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}
func TestPrivatePostgresBorrowLifecycle(t *testing.T) {
	socket := os.Getenv("ANVIL_PRIVATE_PG_SOCKET")
	if socket == "" {
		t.Skip("requires opt-in private PostgreSQL fixture")
	}
	port := os.Getenv("ANVIL_PRIVATE_PG_PORT")
	base := t.TempDir()
	owner := filepath.Join(base, "owner")
	out, err := exec.Command("git", "init", owner).CombinedOutput()
	require.NoError(t, err, string(out))
	env := "DB_CONNECTION=pgsql\nDB_HOST=" + socket + "\nDB_PORT=" + port + "\nDB_USERNAME=" + os.Getenv("USER") + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(owner, ".env"), []byte(env), 0600))
	factory := NewHerdDatabaseClientFactory(privatePostgresCommander{})
	appCtx := &types.ScaffoldContext{WorktreePath: owner, SiteName: "private_owner", DbSuffix: "fixture_suffix", SiteDriver: "herd"}
	require.NoError(t, NewDbCreateStepWithFactory(config.StepConfig{}, factory).Run(appCtx, types.StepOptions{}))
	app := appCtx.GetVar("DatabaseName")
	require.NotEmpty(t, app)
	require.Equal(t, "true", appCtx.GetVar("ApplicationDatabaseNew"))
	// Real PostgreSQL data must survive the idempotent create retry.
	sql := func(db, query string) string {
		t.Helper()
		out, err := exec.Command("psql", "-h", socket, "-p", port, "-d", db, "-Atc", query).CombinedOutput()
		require.NoError(t, err, string(out))
		return string(out)
	}
	sql(app, "CREATE TABLE sentinel(value text); INSERT INTO sentinel VALUES ('keep')")
	require.NoError(t, NewDbCreateStepWithFactory(config.StepConfig{}, factory).Run(appCtx, types.StepOptions{}))
	require.Equal(t, "false", appCtx.GetVar("ApplicationDatabaseNew"))
	require.Equal(t, "keep\n", sql(app, "SELECT value FROM sentinel"))
	var paths, names []string
	for _, name := range []string{"borrow_one", "borrow_two"} {
		path := filepath.Join(base, name)
		out, err = exec.Command("git", "-C", owner, "worktree", "add", "--orphan", path).CombinedOutput()
		require.NoError(t, err, string(out))
		paths = append(paths, path)
		require.NoError(t, os.WriteFile(filepath.Join(path, ".env"), []byte(env), 0600))
		ctx := &types.ScaffoldContext{WorktreePath: path, SiteName: name, DbSuffix: name + "_suffix", BorrowAppDB: owner, SiteDriver: "herd"}
		require.NoError(t, NewDbCreateStepWithFactory(config.StepConfig{}, factory).Run(ctx, types.StepOptions{}))
		require.Equal(t, app, ctx.GetVar("DatabaseName"))
		require.NoError(t, NewDbCreateStepWithFactory(config.StepConfig{Role: "testing"}, factory).Run(ctx, types.StepOptions{}))
		state, err := config.ReadLocalState(path)
		require.NoError(t, err)
		require.Len(t, state.Databases, 1)
		names = append(names, state.Databases[0].Name)
		require.ErrorContains(t, dblifecycle.CheckCleanup(owner), "borrower")
	}
	require.NotEqual(t, names[0], names[1])
	require.Equal(t, "keep\n", sql(app, "SELECT value FROM sentinel"))
	for _, path := range paths {
		require.NoError(t, NewDbDestroyStepWithFactory(config.StepConfig{}, factory).Run(&types.ScaffoldContext{WorktreePath: path}, types.StepOptions{}))
		out, err = exec.Command("git", "-C", owner, "worktree", "remove", "--force", path).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	require.NoError(t, NewDbDestroyStepWithFactory(config.StepConfig{}, factory).Run(appCtx, types.StepOptions{}))
}
