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
	// Git reports canonical paths (long names on Windows, resolved symlinks
	// such as /private/var on macOS); build expectations in the same form.
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
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

// A sibling deleted from disk but still registered with Git cannot hold any
// borrow record, so it must not block an unrelated worktree's removal.
func TestCheckCleanupSkipsDeletedSiblingWithWarning(t *testing.T) {
	owner, sibling := gitFixture(t)
	require.NoError(t, os.RemoveAll(sibling))
	var warnings []string
	require.NoError(t, CheckCleanupWithWarnings(owner, func(message string) { warnings = append(warnings, message) }))
	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], sibling)
	require.Contains(t, warnings[0], "git worktree prune")
	require.Contains(t, warnings[0], "git worktree repair")
	require.NoError(t, CheckCleanup(owner))
}

// A sibling directory without .anvil.local has no records and never blocks.
func TestCheckCleanupAllowsSiblingWithoutLocalState(t *testing.T) {
	owner, _ := gitFixture(t)
	var warnings []string
	require.NoError(t, CheckCleanupWithWarnings(owner, func(message string) { warnings = append(warnings, message) }))
	require.Empty(t, warnings)
}

// An existing sibling whose .anvil.local cannot be read may hold a borrow
// record, so cleanup fails closed. A directory in place of the file is
// unreadable even for root.
func TestCheckCleanupFailsClosedForUnreadableSibling(t *testing.T) {
	owner, sibling := gitFixture(t)
	require.NoError(t, config.WriteLocalState(owner, config.LocalState{Databases: []config.OwnedDatabase{{Name: "app", Engine: "mysql", Endpoint: &config.DatabaseEndpoint{Driver: "yerd", Service: "mysql", Host: "127.0.0.1", Port: "3306"}, Role: "application"}}}))
	require.NoError(t, os.Mkdir(filepath.Join(sibling, config.LocalStateFile), 0o755))
	require.ErrorContains(t, CheckCleanupWithWarnings(owner, func(string) {}), "cannot inspect sibling")
}

// A registered path that is a dangling symlink (for example an unmounted
// volume) may still hold records elsewhere: fail closed rather than skip.
func TestCheckCleanupFailsClosedForDanglingSymlinkSibling(t *testing.T) {
	owner, sibling := gitFixture(t)
	moved := sibling + "-moved"
	require.NoError(t, os.Rename(sibling, moved))
	require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "gone"), sibling))
	require.ErrorContains(t, CheckCleanupWithWarnings(owner, func(string) {}), "cannot inspect sibling")
}

// pathMissing proves absence only when the nearest existing ancestor is a
// real directory; Git canonicalizes registered paths, so this is unit-tested.
func TestPathMissingDistinguishesAbsenceFromUnresolvablePaths(t *testing.T) {
	base := t.TempDir()
	file := filepath.Join(base, "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	dangling := filepath.Join(base, "dangling")
	require.NoError(t, os.Symlink(filepath.Join(base, "gone"), dangling))
	for name, tc := range map[string]struct {
		path    string
		missing bool
		wantErr bool
	}{
		"missing child":               {path: filepath.Join(base, "absent"), missing: true},
		"missing with missing parent": {path: filepath.Join(base, "absent", "deeper", "wt"), missing: true},
		"existing directory":          {path: base},
		"dangling symlink":            {path: dangling},
		"below dangling ancestor":     {path: filepath.Join(dangling, "wt"), wantErr: true},
		"below regular file":          {path: filepath.Join(file, "wt"), wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			missing, err := pathMissing(tc.path)
			require.Equal(t, tc.missing, missing)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// Git never prunes a locked entry: a locked worktree on unmounted removable
// media is expected to come back, with its borrow record. Fail closed.
func TestCheckCleanupFailsClosedForMissingLockedSibling(t *testing.T) {
	owner, sibling := gitFixture(t)
	out, err := exec.Command("git", "-C", owner, "worktree", "lock", "--reason", "usb drive", sibling).CombinedOutput()
	require.NoError(t, err, string(out))
	require.NoError(t, os.RemoveAll(sibling))
	var warnings []string
	err = CheckCleanupWithWarnings(owner, func(message string) { warnings = append(warnings, message) })
	require.ErrorContains(t, err, "is locked (usb drive) but absent; reconnect it, or unlock and prune it")
	require.ErrorContains(t, err, sibling)
	require.Empty(t, warnings)
}

func TestRegisteredWorktreesReportsLocks(t *testing.T) {
	owner, sibling := gitFixture(t)
	out, err := exec.Command("git", "-C", owner, "worktree", "lock", sibling).CombinedOutput()
	require.NoError(t, err, string(out))
	worktrees, err := RegisteredWorktrees(owner)
	require.NoError(t, err)
	require.Len(t, worktrees, 2)
	require.False(t, worktrees[0].Locked)
	require.Equal(t, RegisteredWorktree{Path: sibling, Locked: true}, worktrees[1])
}
