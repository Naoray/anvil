package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/dblifecycle"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/presets"
	"github.com/naoray/anvil/internal/scaffold"
)

func TestDatabaseFlagsReplaceOnlySameConfigSelector(t *testing.T) {
	cmd := &cobra.Command{}
	addDatabaseFlags(cmd)
	require.NoError(t, cmd.ParseFlags([]string{"--db-action=application=migrate", "--db-action=testing=skip", "--reset-database=history"}))
	pc := &ProjectContext{Config: &config.Config{Scaffold: config.ScaffoldConfig{DatabaseActions: map[string]string{"application": "skip", "other": "migrate"}}}}
	require.NoError(t, applyDatabaseFlags(cmd, pc))
	require.Equal(t, map[string]string{"application": "migrate", "testing": "skip", "other": "migrate"}, pc.Config.Scaffold.DatabaseActions)
	require.Equal(t, []string{"history"}, pc.Config.Scaffold.ResetDatabases)
}
func TestRemoveLifecycleRefusesBorrowerBeforeCleanupAndKeepDBRetainsBinding(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	out, err := exec.Command("git", "init", repo).CombinedOutput()
	require.NoError(t, err, string(out))
	owner := filepath.Join(base, "owner")
	borrower := filepath.Join(base, "borrower")
	for _, path := range []string{owner, borrower} {
		out, err = exec.Command("git", "-C", repo, "worktree", "add", "--orphan", path).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	canonical, err := dblifecycle.Canonical(owner)
	require.NoError(t, err)
	require.NoError(t, config.WriteLocalState(owner, config.LocalState{Databases: []config.OwnedDatabase{{Name: "shared_app", Engine: "mysql", Endpoint: &config.DatabaseEndpoint{Driver: "yerd", Service: "mysql", Host: "127.0.0.1", Port: "3306"}, Role: "application"}}}))
	binding := &config.BorrowedApplication{SourcePath: canonical, Name: "shared_app", Engine: "mysql", Driver: "yerd", Service: "mysql", Host: "127.0.0.1", Port: "3306"}
	require.NoError(t, config.WriteLocalState(borrower, config.LocalState{BorrowedApplication: binding, Databases: []config.OwnedDatabase{{Name: "borrow_test", Engine: "mysql", Endpoint: &config.DatabaseEndpoint{Driver: "yerd", Service: "mysql", Host: "127.0.0.1", Port: "3306"}, Role: "testing"}}}))
	marker := filepath.Join(base, "cleanup-effect")
	pc := &ProjectContext{GitDir: filepath.Join(repo, ".git"), Config: &config.Config{Cleanup: config.CleanupConfig{Steps: []config.CleanupStep{{Name: "command.run", ConditionHolder: config.ConditionHolder{Condition: map[string]any{"command": "touch " + marker}}}}}}}
	deps := removeLifecycleDependencies{lifecycleLock: dblifecycle.Lock, resolvePreset: func(*ProjectContext, string, string) presets.ResolvedPreset {
		return presets.NewManager().Resolve("php", "", owner)
	}, scaffoldManager: func(*ProjectContext) *scaffold.ScaffoldManager { return scaffold.NewScaffoldManager() }, removeWorktree: git.RemoveWorktree}
	wt := git.Worktree{Path: owner, Branch: "owner"}
	require.ErrorContains(t, runRemoveLifecycle(pc, wt, removeLifecycleOptions{Quiet: true}, scaffold.CleanupOptions{}, nil, deps), "borrower")
	require.NoFileExists(t, marker)
	require.DirExists(t, owner)
	require.NoError(t, runRemoveLifecycle(pc, wt, removeLifecycleOptions{Quiet: true}, scaffold.CleanupOptions{SkipDatabaseCleanup: true}, nil, deps))
	require.NoDirExists(t, owner)
	app, testDB, err := resolveExecDatabases(borrower)
	require.NoError(t, err)
	require.Equal(t, "shared_app", app)
	require.Equal(t, "borrow_test", testDB)
	state, err := config.ReadLocalState(borrower)
	require.NoError(t, err)
	require.Equal(t, binding, state.BorrowedApplication)
}

func TestDatabaseBinaryBorrowerExecAndOwnerRemovalGuard(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	out, err := exec.Command("git", "init", "-b", "main", repo).CombinedOutput()
	require.NoError(t, err, string(out))
	worktreeBase := filepath.Join(base, "worktrees")
	owner := filepath.Join(worktreeBase, "demo", "owner")
	borrower := filepath.Join(worktreeBase, "demo", "borrower")
	require.NoError(t, os.MkdirAll(filepath.Dir(owner), 0755))
	for _, path := range []string{owner, borrower} {
		out, err = exec.Command("git", "-C", repo, "worktree", "add", "--orphan", path).CombinedOutput()
		require.NoError(t, err, string(out))
	}
	canonical, err := dblifecycle.Canonical(owner)
	require.NoError(t, err)
	require.NoError(t, config.WriteLocalState(owner, config.LocalState{Databases: []config.OwnedDatabase{{Name: "shared_app", Engine: "mysql", Endpoint: &config.DatabaseEndpoint{Driver: "yerd", Service: "mysql", Host: "127.0.0.1", Port: "3306"}, Role: "application"}}}))
	require.NoError(t, config.WriteLocalState(borrower, config.LocalState{BorrowedApplication: &config.BorrowedApplication{SourcePath: canonical, Name: "shared_app", Engine: "mysql", Driver: "yerd", Service: "mysql", Host: "127.0.0.1", Port: "3306"}, Databases: []config.OwnedDatabase{{Name: "isolated_test", Engine: "mysql", Endpoint: &config.DatabaseEndpoint{Driver: "yerd", Service: "mysql", Host: "127.0.0.1", Port: "3306"}, Role: "testing"}}}))
	configHome := filepath.Join(base, "config")
	require.NoError(t, os.MkdirAll(filepath.Join(configHome, "anvil"), 0755))
	yaml := fmt.Sprintf("setup_complete: true\nsite_driver: yerd\nworktree_base: %q\nprojects:\n  demo:\n    path: %q\n    preset: php\n    default_branch: main\n", worktreeBase, repo)
	require.NoError(t, os.WriteFile(filepath.Join(configHome, "anvil", "anvil.yaml"), []byte(yaml), 0600))
	cmd := exec.Command(getAnvilBinary(t), "remove", "owner", "--force", "--no-interactive")
	cmd.Dir = repo
	cmd.Env = subprocessEnv(t, "XDG_CONFIG_HOME="+configHome)
	out, err = cmd.CombinedOutput()
	require.Error(t, err, string(out))
	require.Contains(t, string(out), "borrower")
	require.DirExists(t, owner)
	cmd = exec.Command(getAnvilBinary(t), "remove", "owner", "--force", "--keep-db", "--no-interactive")
	cmd.Dir = repo
	cmd.Env = subprocessEnv(t, "XDG_CONFIG_HOME="+configHome)
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.NoDirExists(t, owner)
	cmd = exec.Command(getAnvilBinary(t), "exec", "--", helperSelf(t))
	cmd.Dir = borrower
	cmd.Env = subprocessEnv(t, "ANVIL_TEST_HELPER=1", "ANVIL_HELPER_MODE=env")
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "DB_DATABASE=isolated_test\n")
	require.Contains(t, string(out), "ANVIL_DB_DATABASE=shared_app\n")
}

func TestDatabaseFlagsAcceptCanonicalSourcePathAlias(t *testing.T) {
	source := t.TempDir()
	alias := filepath.Join(t.TempDir(), "source-alias")
	if err := os.Symlink(source, alias); err != nil {
		t.Skipf("symlink fixture unavailable: %v", err)
	}
	cmd := &cobra.Command{}
	addDatabaseFlags(cmd)
	require.NoError(t, cmd.ParseFlags([]string{"--borrow-app-db", alias}))
	pc := &ProjectContext{Config: &config.Config{}}
	require.NoError(t, applyDatabaseFlags(cmd, pc))
	canonical, err := dblifecycle.Canonical(source)
	require.NoError(t, err)
	require.Equal(t, canonical, pc.Config.Scaffold.BorrowAppDB)
}
