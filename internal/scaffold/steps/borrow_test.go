package steps

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/naoray/anvil/internal/dblifecycle"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/scaffold/types"
)

func TestBorrowRejectsUnregisteredAndChains(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, exec.Command("git", "init", root).Run())
	ctx := &types.ScaffoldContext{WorktreePath: root, SiteDriver: "yerd", BorrowAppDB: t.TempDir()}
	_, err := (&DbCreateStep{}).borrowApplication(ctx, types.StepOptions{})
	require.ErrorContains(t, err, "registered")
	ctx.BorrowAppDB = root
	_, err = (&DbCreateStep{}).borrowApplication(ctx, types.StepOptions{})
	require.ErrorContains(t, err, "itself")
}
func TestBorrowedExecStateNeverBecomesOwned(t *testing.T) {
	root := t.TempDir()
	b := &config.BorrowedApplication{SourcePath: filepath.Join(root, "source"), Name: "app_shared", Engine: "mysql", Driver: "yerd", Service: "mysql", Host: "127.0.0.1", Port: "3306"}
	require.NoError(t, config.WriteLocalState(root, config.LocalState{DbSuffix: "own_suffix", BorrowedApplication: b, Databases: []config.OwnedDatabase{{Name: "my_test", Engine: "mysql", Endpoint: &config.DatabaseEndpoint{Driver: "yerd", Service: "mysql", Host: "127.0.0.1", Port: "3306"}, Role: "testing"}}}))
	state, err := config.ReadLocalState(root)
	require.NoError(t, err)
	require.Equal(t, b, state.BorrowedApplication)
	require.Len(t, state.Databases, 1)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("DB_CONNECTION=mysql\nDB_DATABASE=app_shared\n"), 0600))
	ctx := &types.ScaffoldContext{WorktreePath: root, ResetDatabases: []string{"app_shared"}}
	_, err = planDatabaseActions(ctx)
	require.ErrorContains(t, err, "borrowed/shared")
	ctx.ResetDatabases = nil
	actions, err := planDatabaseActions(ctx)
	require.NoError(t, err)
	require.Equal(t, "skip", actions[0].Action)
}

func TestTwoBorrowersKeepIndependentOwnedTestingAndHistory(t *testing.T) {
	base := t.TempDir()
	owner := filepath.Join(base, "owner")
	out, err := exec.Command("git", "init", owner).CombinedOutput()
	require.NoError(t, err, string(out))
	require.NoError(t, os.WriteFile(filepath.Join(owner, ".env"), []byte("DB_CONNECTION=mysql\n"), 0600))
	require.NoError(t, config.WriteLocalState(owner, config.LocalState{Databases: []config.OwnedDatabase{{Name: "shared_app", Engine: "mysql", Endpoint: &config.DatabaseEndpoint{Driver: "yerd", Service: "mysql", Host: "127.0.0.1", Port: "3306"}, Role: "application"}, {Name: "old_app", Engine: "mysql", Endpoint: &config.DatabaseEndpoint{Driver: "yerd", Service: "mysql", Host: "127.0.0.1", Port: "3306"}, Role: "auxiliary"}}}))
	client := NewMockDatabaseClient()
	client.AddDatabase("shared_app")
	var testingNames []string
	for _, name := range []string{"one", "two"} {
		path := filepath.Join(base, name)
		out, err := exec.Command("git", "-C", owner, "worktree", "add", "--orphan", path).CombinedOutput()
		require.NoError(t, err, string(out))
		require.NoError(t, os.WriteFile(filepath.Join(path, ".env"), []byte("DB_CONNECTION=mysql\n"), 0600))
		ctx := &types.ScaffoldContext{WorktreePath: path, SiteName: name, DbSuffix: name + "_suffix", BorrowAppDB: owner}
		step := NewDbCreateStepWithFactory(config.StepConfig{}, MockClientFactory(client))
		require.NoError(t, step.Run(ctx, types.StepOptions{}))
		require.Equal(t, "shared_app", ctx.SnapshotForTemplate()["DatabaseName"])
		testStep := NewDbCreateStepWithFactory(config.StepConfig{Role: "testing"}, MockClientFactory(client))
		require.NoError(t, testStep.Run(ctx, types.StepOptions{}))
		state, err := config.ReadLocalState(path)
		require.NoError(t, err)
		require.Len(t, state.Databases, 1)
		testingNames = append(testingNames, state.Databases[0].Name)
		ctx.BorrowAppDB = ""
		require.NoError(t, step.Run(ctx, types.StepOptions{}))
		again, err := config.ReadLocalState(path)
		require.NoError(t, err)
		require.Equal(t, state, again)
		destroy := NewDbDestroyStepWithFactory(config.StepConfig{}, MockClientFactory(client))
		require.NoError(t, destroy.Run(ctx, types.StepOptions{}))
		require.True(t, client.HasDatabase("shared_app"))
	}
	require.NotEqual(t, testingNames[0], testingNames[1])
	require.NotContains(t, client.GetCreateCalls(), "shared_app")
	ownerState, err := config.ReadLocalState(owner)
	require.NoError(t, err)
	require.Len(t, ownerState.Databases, 2)
	destroy := NewDbDestroyStepWithFactory(config.StepConfig{}, MockClientFactory(client))
	require.ErrorContains(t, destroy.Run(&types.ScaffoldContext{WorktreePath: owner}, types.StepOptions{}), "borrower")
}

func TestBorrowRemoveSeparateProcesses(t *testing.T) {
	if mode := os.Getenv("ANVIL_DB_CHILD_MODE"); mode != "" {
		path := os.Getenv("ANVIL_DB_CHILD_TARGET")
		owner := os.Getenv("ANVIL_DB_CHILD_OWNER")
		require.NoError(t, os.WriteFile(os.Getenv("ANVIL_DB_CHILD_STARTED"), nil, 0600))
		release, err := dblifecycle.Lock(path)
		require.NoError(t, err)
		if mode == "borrow" {
			client := NewMockDatabaseClient()
			client.AddDatabase("shared_app")
			ctx := &types.ScaffoldContext{WorktreePath: path, BorrowAppDB: owner, DatabaseLockHeld: true, SiteDriver: "yerd"}
			err = NewDbCreateStepWithFactory(config.StepConfig{}, MockClientFactory(client)).Run(ctx, types.StepOptions{})
		} else {
			err = dblifecycle.CheckCleanup(path)
			if err == nil {
				out, removeErr := exec.Command("git", "-C", os.Getenv("ANVIL_DB_CHILD_REPO"), "worktree", "remove", "--force", path).CombinedOutput()
				if removeErr != nil {
					err = fmt.Errorf("%w: %s", removeErr, out)
				}
			}
		}
		result := "ok"
		if err != nil {
			result = err.Error()
		}
		require.NoError(t, os.WriteFile(os.Getenv("ANVIL_DB_CHILD_RESULT"), []byte(result), 0600))
		if hold := os.Getenv("ANVIL_DB_CHILD_HOLD"); hold != "" {
			deadline := time.Now().Add(10 * time.Second)
			for {
				if _, err := os.Stat(hold); err == nil {
					break
				}
				require.True(t, time.Now().Before(deadline), "release signal timed out")
				time.Sleep(10 * time.Millisecond)
			}
		}
		require.NoError(t, release())
		return
	}
	for _, first := range []string{"borrow", "remove"} {
		t.Run(first+" wins", func(t *testing.T) {
			base := t.TempDir()
			repo := filepath.Join(base, "repo")
			out, err := exec.Command("git", "init", repo).CombinedOutput()
			require.NoError(t, err, string(out))
			owner := filepath.Join(base, "owner")
			borrower := filepath.Join(base, "borrower")
			for _, path := range []string{owner, borrower} {
				out, err = exec.Command("git", "-C", repo, "worktree", "add", "--orphan", path).CombinedOutput()
				require.NoError(t, err, string(out))
				require.NoError(t, os.WriteFile(filepath.Join(path, ".env"), []byte("DB_CONNECTION=mysql\n"), 0600))
			}
			require.NoError(t, config.WriteLocalState(owner, config.LocalState{Databases: []config.OwnedDatabase{{Name: "shared_app", Role: "application", Engine: "mysql", Endpoint: &config.DatabaseEndpoint{Driver: "yerd", Service: "mysql", Host: "127.0.0.1", Port: "3306"}}}}))
			hold := filepath.Join(base, "release")
			start := func(mode string, wait bool) (*exec.Cmd, string, string) {
				path := owner
				if mode == "borrow" {
					path = borrower
				}
				result := filepath.Join(base, mode+"-result")
				started := filepath.Join(base, mode+"-started")
				cmd := exec.Command(os.Args[0], "-test.run=^TestBorrowRemoveSeparateProcesses$")
				cmd.Env = append(os.Environ(), "ANVIL_DB_CHILD_MODE="+mode, "ANVIL_DB_CHILD_TARGET="+path, "ANVIL_DB_CHILD_OWNER="+owner, "ANVIL_DB_CHILD_REPO="+repo, "ANVIL_DB_CHILD_RESULT="+result, "ANVIL_DB_CHILD_STARTED="+started)
				if wait {
					cmd.Env = append(cmd.Env, "ANVIL_DB_CHILD_HOLD="+hold)
				}
				require.NoError(t, cmd.Start())
				return cmd, result, started
			}
			waitFile := func(path string) {
				require.Eventually(t, func() bool { _, err := os.Stat(path); return err == nil }, 10*time.Second, 10*time.Millisecond)
			}
			one, resultOne, _ := start(first, true)
			waitFile(resultOne)
			second := "remove"
			if first == "remove" {
				second = "borrow"
			}
			two, resultTwo, startedTwo := start(second, false)
			waitFile(startedTwo)
			// Both actors are real processes using distinct linked-worktree paths.
			time.Sleep(100 * time.Millisecond)
			require.NoFileExists(t, resultTwo)
			require.NoError(t, os.WriteFile(hold, nil, 0600))
			require.NoError(t, one.Wait())
			require.NoError(t, two.Wait())
			data, err := os.ReadFile(resultOne)
			require.NoError(t, err)
			require.Equal(t, "ok", string(data))
			data, err = os.ReadFile(resultTwo)
			require.NoError(t, err)
			if first == "borrow" {
				require.Contains(t, string(data), "borrower")
				require.DirExists(t, owner)
			} else {
				require.Contains(t, string(data), "unavailable")
				state, err := config.ReadLocalState(borrower)
				require.NoError(t, err)
				require.Nil(t, state.BorrowedApplication)
			}
		})
	}
}

func TestBorrowRejectsMalformedAbsentChainedAndMismatchedSources(t *testing.T) {
	for _, scenario := range []string{"malformed", "missing app", "chain", "mariadb mismatch", "engine mismatch", "absent database"} {
		t.Run(scenario, func(t *testing.T) {
			base := t.TempDir()
			owner := filepath.Join(base, "owner")
			out, err := exec.Command("git", "init", owner).CombinedOutput()
			require.NoError(t, err, string(out))
			target := filepath.Join(base, "target")
			out, err = exec.Command("git", "-C", owner, "worktree", "add", "--orphan", target).CombinedOutput()
			require.NoError(t, err, string(out))
			ownerEnv := "DB_CONNECTION=mysql\n"
			targetEnv := ownerEnv
			state := "databases:\n  - {name: shared_app, engine: mysql, role: application}\n"
			switch scenario {
			case "malformed":
				state = "databases: ["
			case "missing app":
				state = "db_suffix: own_suffix\n"
			case "chain":
				state += "borrowed_application: {name: other_app}\n"
			case "mariadb mismatch":
				targetEnv = "DB_CONNECTION=mariadb\n"
			case "engine mismatch":
				targetEnv = "DB_CONNECTION=pgsql\n"
			}
			require.NoError(t, os.WriteFile(filepath.Join(owner, ".env"), []byte(ownerEnv), 0600))
			require.NoError(t, os.WriteFile(filepath.Join(target, ".env"), []byte(targetEnv), 0600))
			require.NoError(t, os.WriteFile(filepath.Join(owner, config.LocalStateFile), []byte(state), 0600))
			client := NewMockDatabaseClient()
			if scenario != "absent database" {
				client.AddDatabase("shared_app")
			}
			ctx := &types.ScaffoldContext{WorktreePath: target, BorrowAppDB: owner}
			require.Error(t, NewDbCreateStepWithFactory(config.StepConfig{}, MockClientFactory(client)).Run(ctx, types.StepOptions{}))
			after, err := config.ReadLocalState(target)
			require.NoError(t, err)
			require.Nil(t, after.BorrowedApplication)
			require.Empty(t, client.GetCreateCalls())
			require.Empty(t, client.GetDropCalls())
		})
	}
}
