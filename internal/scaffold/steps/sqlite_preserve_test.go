package steps

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/scaffold/types"
)

func TestSQLitePreservesRowsAndTemplatePath(t *testing.T) {
	sqlite, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 unavailable")
	}
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "database"), 0755))
	path := filepath.Join(root, "database", "custom.sqlite")
	out, err := exec.Command(sqlite, path, "CREATE TABLE sentinel(value); INSERT INTO sentinel VALUES ('keep');").CombinedOutput()
	require.NoError(t, err, string(out))
	ctx := &types.ScaffoldContext{WorktreePath: root, DbSuffix: "wrong_suffix"}
	step := &DbCreateStep{}
	for range 2 {
		require.NoError(t, step.createSqlite(ctx, "database/custom.sqlite", types.StepOptions{}))
	}
	out, err = exec.Command(sqlite, path, "SELECT value FROM sentinel;").CombinedOutput()
	require.NoError(t, err, string(out))
	require.Equal(t, "keep\n", string(out))
	require.Equal(t, "database/custom.sqlite", ctx.SnapshotForTemplate()["DatabaseName"])
}

func TestSQLiteRejectsUnsafePathsBeforeMutation(t *testing.T) {
	for _, name := range []string{"../outside.sqlite", ":memory:", "file:db?mode=memory", "/tmp/anvil-outside.sqlite"} {
		t.Run(name, func(t *testing.T) {
			ctx := &types.ScaffoldContext{WorktreePath: t.TempDir()}
			require.Error(t, (&DbCreateStep{}).createSqlite(ctx, name, types.StepOptions{DryRun: true}))
		})
	}
	root := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "database")))
	require.Error(t, (&DbCreateStep{}).createSqlite(&types.ScaffoldContext{WorktreePath: root}, "database/new.sqlite", types.StepOptions{}))
	_, err := os.Stat(filepath.Join(outside, "new.sqlite"))
	require.True(t, os.IsNotExist(err))
}
