package dblifecycle

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
)

func gitFixture(t *testing.T) (string, string) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	out, err := exec.Command("git", "init", root).CombinedOutput()
	require.NoError(t, err, string(out))
	sibling := filepath.Join(base, "borrower")
	out, err = exec.Command("git", "-C", root, "worktree", "add", "--orphan", sibling).CombinedOutput()
	require.NoError(t, err, string(out))
	return root, sibling
}
func TestBorrowCleanupProtectsHistoricalRecordsAndUnreadableSiblings(t *testing.T) {
	owner, borrower := gitFixture(t)
	canonical, err := Canonical(owner)
	require.NoError(t, err)
	require.NoError(t, config.WriteLocalState(owner, config.LocalState{Databases: []config.OwnedDatabase{{Name: "old_app", Engine: "mysql", Endpoint: &config.DatabaseEndpoint{Driver: "yerd", Service: "mysql", Host: "127.0.0.1", Port: "3306"}, Role: "auxiliary"}, {Name: "new_app", Engine: "mysql", Endpoint: &config.DatabaseEndpoint{Driver: "yerd", Service: "mysql", Host: "127.0.0.1", Port: "3306"}, Role: "application"}}}))
	require.NoError(t, config.WriteLocalState(borrower, config.LocalState{BorrowedApplication: &config.BorrowedApplication{SourcePath: canonical, Name: "old_app", Engine: "mysql", Driver: "yerd", Service: "mysql", Host: "127.0.0.1", Port: "3306"}}))
	require.ErrorContains(t, CheckCleanup(owner), "borrower")
	require.NoError(t, CheckCleanup(borrower))
	require.NoError(t, os.WriteFile(filepath.Join(borrower, config.LocalStateFile), []byte("bad: ["), 0600))
	require.ErrorContains(t, CheckCleanup(owner), "cannot inspect sibling")
}
func TestCrossProcessCommonDirectoryLock(t *testing.T) {
	if path := os.Getenv("ANVIL_LOCK_CHILD"); path != "" {
		release, err := Lock(path)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(os.Getenv("ANVIL_LOCK_MARKER"), []byte("acquired"), 0600))
		require.NoError(t, release())
		return
	}
	root, sibling := gitFixture(t)
	alias := filepath.Join(t.TempDir(), "alias")
	require.NoError(t, os.Symlink(sibling, alias))
	one, err := CommonDir(root)
	require.NoError(t, err)
	two, err := CommonDir(alias)
	require.NoError(t, err)
	require.Equal(t, one, two)
	for _, pair := range [][2]string{{root, alias}, {alias, root}} {
		release, err := Lock(pair[0])
		require.NoError(t, err)
		marker := filepath.Join(t.TempDir(), "acquired")
		cmd := exec.Command(os.Args[0], "-test.run=^TestCrossProcessCommonDirectoryLock$")
		cmd.Env = append(os.Environ(), "ANVIL_LOCK_CHILD="+pair[1], "ANVIL_LOCK_MARKER="+marker)
		require.NoError(t, cmd.Start())
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			t.Fatalf("child failed to contend: %v", err)
		case <-time.After(150 * time.Millisecond):
		}
		_, err = os.Stat(marker)
		require.True(t, os.IsNotExist(err))
		require.NoError(t, release())
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(10 * time.Second):
			require.NoError(t, cmd.Process.Kill())
			t.Fatal("child failed to acquire released lock")
		}
		require.FileExists(t, marker)
	}
}
