package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/presets"
	"github.com/naoray/anvil/internal/scaffold"
	"github.com/naoray/anvil/internal/scaffold/steps"
)

func resolvedRemoveTestPreset() presets.ResolvedPreset {
	manager := presets.NewManager()
	manager.Register(removeTestPreset{})
	return manager.Resolve("remove-test", "", "")
}

// makeTestProject creates a git repo with a local "origin" remote and returns
// a ProjectContext ready for pruneProject.
func makeTestProject(t *testing.T, name string) (*ProjectContext, string) {
	t.Helper()
	tmp := t.TempDir()

	// bare remote
	remoteDir := filepath.Join(tmp, "remote.git")
	runGitCmd(t, tmp, "init", "--bare", "-b", "main", remoteDir)

	// local repo
	repoDir := filepath.Join(tmp, "repo")
	require.NoError(t, os.MkdirAll(repoDir, 0755))
	runGitCmd(t, repoDir, "init", "-b", "main")
	runGitCmd(t, repoDir, "config", "user.email", "test@example.com")
	runGitCmd(t, repoDir, "config", "user.name", "Test User")
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("init"), 0644))
	runGitCmd(t, repoDir, "add", ".")
	runGitCmd(t, repoDir, "commit", "-m", "initial")
	runGitCmd(t, repoDir, "remote", "add", "origin", remoteDir)
	runGitCmd(t, repoDir, "push", "origin", "main")

	gitDir := filepath.Join(repoDir, ".git")
	globalCfg := &config.GlobalConfig{
		Projects: map[string]*config.ProjectInfo{
			name: {Path: repoDir, DefaultBranch: "main"},
		},
	}
	pc := &ProjectContext{
		CWD:           repoDir,
		GitDir:        gitDir,
		ProjectPath:   repoDir,
		Config:        &config.Config{DefaultBranch: "main"},
		DefaultBranch: "main",
		ProjectName:   name,
		GlobalConfig:  globalCfg,
	}
	return pc, repoDir
}

// addMergedWorktree adds a feature branch, commits, merges into main, pushes, returns worktree path.
func addMergedWorktree(t *testing.T, pc *ProjectContext, branch string) string {
	t.Helper()
	tmp := t.TempDir()
	wtPath := filepath.Join(tmp, branch)

	require.NoError(t, git.CreateWorktree(pc.GitDir, wtPath, branch, "main"))
	runGitCmd(t, wtPath, "config", "user.email", "test@example.com")
	runGitCmd(t, wtPath, "config", "user.name", "Test User")
	require.NoError(t, os.WriteFile(filepath.Join(wtPath, "f.txt"), []byte(branch), 0644))
	runGitCmd(t, wtPath, "add", ".")
	runGitCmd(t, wtPath, "commit", "-m", "feature commit")

	// Merge into main
	runGitCmd(t, pc.ProjectPath, "merge", branch, "--no-ff", "-m", "Merge "+branch)
	// Push merged main to origin so origin/main is ahead
	runGitCmd(t, pc.ProjectPath, "push", "origin", "main")

	return wtPath
}

func TestPruneProject_RemovesMergedWorktree(t *testing.T) {
	pc, _ := makeTestProject(t, "alpha")
	wtPath := addMergedWorktree(t, pc, "feature-merged")

	// Verify worktree exists before prune
	_, err := os.Stat(wtPath)
	require.NoError(t, err, "worktree should exist before prune")

	err = pruneProject(pc, true, false, false, false)
	require.NoError(t, err)

	_, err = os.Stat(wtPath)
	assert.True(t, os.IsNotExist(err), "merged worktree should be removed after pruneProject")
}

func TestPruneProject_KeepsUnmergedWorktree(t *testing.T) {
	pc, repoDir := makeTestProject(t, "beta")

	// Add an unmerged feature branch worktree
	tmp := t.TempDir()
	wtPath := filepath.Join(tmp, "feature-open")
	require.NoError(t, git.CreateWorktree(pc.GitDir, wtPath, "feature-open", "main"))
	runGitCmd(t, wtPath, "config", "user.email", "test@example.com")
	runGitCmd(t, wtPath, "config", "user.name", "Test User")
	require.NoError(t, os.WriteFile(filepath.Join(wtPath, "f.txt"), []byte("open"), 0644))
	runGitCmd(t, wtPath, "add", ".")
	runGitCmd(t, wtPath, "commit", "-m", "open work")
	_ = repoDir

	err := pruneProject(pc, true, false, false, false)
	require.NoError(t, err)

	_, err = os.Stat(wtPath)
	assert.NoError(t, err, "unmerged worktree should still exist after pruneProject")
}

func TestPruneProject_DryRunDoesNotRemove(t *testing.T) {
	pc, _ := makeTestProject(t, "gamma")
	wtPath := addMergedWorktree(t, pc, "feature-dry")

	previousStdout := os.Stdout
	output := captureStdout(t, func() {
		err := pruneProject(pc, true, true /* dryRun */, false, false)
		require.NoError(t, err)
	})
	require.Same(t, previousStdout, os.Stdout)

	_, err := os.Stat(wtPath)
	assert.NoError(t, err, "dry-run should not remove the worktree")
	assert.NotContains(t, output, "Removed:")
}

func TestCaptureStdoutRestoresAfterCallbackPanic(t *testing.T) {
	previousStdout := os.Stdout
	defer func() {
		if os.Stdout == previousStdout {
			return
		}
		if err := os.Stdout.Close(); err != nil {
			t.Errorf("closing leaked stdout: %v", err)
		}
		os.Stdout = previousStdout
	}()

	func() {
		defer func() { require.NotNil(t, recover()) }()
		captureStdout(t, func() { panic("callback failed") })
	}()

	assert.Same(t, previousStdout, os.Stdout)
}

func TestPruneProject_DoesNotRemoveAfterCleanupFailure(t *testing.T) {
	worktreePath := t.TempDir()
	require.NoError(t, config.WriteLocalState(worktreePath, config.LocalState{Databases: []config.OwnedDatabase{
		{Name: "app_feature_failed", Engine: "mysql", Role: config.DbRoleApplication},
	}}))

	client := steps.NewMockDatabaseClient()
	client.SetDropError(errors.New("database cleanup failed"))
	client.AddDatabase("app_feature_failed")
	manager := scaffold.NewScaffoldManagerWithRegistry(&removeTestRegistry{
		client: client,
		output: &bytes.Buffer{},
	})
	resolvedPreset := resolvedRemoveTestPreset()

	pc := &ProjectContext{
		GitDir:        "git-dir",
		DefaultBranch: "main",
		Config:        &config.Config{Preset: "remove-test"},
	}
	removeCalls := 0
	deps := pruneProjectDependencies{
		fetchOrigin: func(string) error { return nil },
		listWorktrees: func(string) ([]git.Worktree, error) {
			return []git.Worktree{{Path: worktreePath, Branch: "feature-failed"}}, nil
		},
		isMerged: func(string, string, string) (bool, error) { return true, nil },
		removeLifecycle: removeLifecycleDependencies{
			readLocalState: func(string) (*config.LocalState, error) {
				return &config.LocalState{Databases: []config.OwnedDatabase{
					{Name: "app_feature_failed", Engine: "mysql", Role: config.DbRoleApplication},
				}}, nil
			},
			scaffoldManager: func(*ProjectContext) *scaffold.ScaffoldManager { return manager },
			resolvePreset:   func(*ProjectContext, string, string) presets.ResolvedPreset { return resolvedPreset },
			removeWorktree: func(string, string, bool) error {
				removeCalls++
				return nil
			},
		},
	}

	err := pruneProjectWithDependencies(pc, true, false, false, false, false, deps)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "database cleanup failed")
	assert.Zero(t, removeCalls, "Git removal must not run after cleanup failure")
}

func TestRunPrune_SortsProjectsContinuesAfterFailuresAndJoinsErrors(t *testing.T) {
	openErr := errors.New("cannot open alpha")
	pruneErr := errors.New("cannot prune beta")
	var opened []string
	var visited []string

	globalCfg := &config.GlobalConfig{Projects: map[string]*config.ProjectInfo{
		"charlie": {Path: "/projects/charlie"},
		"alpha":   {Path: "/projects/alpha"},
		"beta":    {Path: "/projects/beta"},
	}}
	deps := pruneCommandDependencies{
		loadGlobalConfig: func() (*config.GlobalConfig, error) { return globalCfg, nil },
		openProject: func(_ string, name string, _ *config.ProjectInfo, _ *config.GlobalConfig) (*ProjectContext, error) {
			opened = append(opened, name)
			if name == "alpha" {
				return nil, openErr
			}
			return &ProjectContext{ProjectName: name}, nil
		},
		pruneProject: func(pc *ProjectContext, _, _, _, _, _ bool) error {
			visited = append(visited, pc.ProjectName)
			if pc.ProjectName == "beta" {
				return pruneErr
			}
			return nil
		},
	}

	root := &cobra.Command{Use: "anvil"}
	root.PersistentFlags().Bool("dry-run", false, "")
	root.PersistentFlags().Bool("verbose", false, "")
	root.PersistentFlags().Bool("quiet", false, "")
	root.AddCommand(newPruneCommand(deps))
	root.SetArgs([]string{"prune", "--force"})

	err := root.Execute()

	require.Error(t, err)
	assert.Equal(t, []string{"alpha", "beta", "charlie"}, opened)
	assert.Equal(t, []string{"beta", "charlie"}, visited)
	assert.ErrorIs(t, err, openErr)
	assert.ErrorIs(t, err, pruneErr)
}

func TestRunPrune_ContinuesAfterFetchFailure(t *testing.T) {
	fetchErr := errors.New("origin unavailable")
	var visited []string
	globalCfg := &config.GlobalConfig{Projects: map[string]*config.ProjectInfo{
		"alpha": {Path: "/projects/alpha"},
		"beta":  {Path: "/projects/beta"},
	}}
	deps := pruneCommandDependencies{
		loadGlobalConfig: func() (*config.GlobalConfig, error) { return globalCfg, nil },
		openProject: func(_ string, name string, _ *config.ProjectInfo, _ *config.GlobalConfig) (*ProjectContext, error) {
			return &ProjectContext{ProjectName: name}, nil
		},
		pruneProject: func(pc *ProjectContext, _, _, _, _, _ bool) error {
			visited = append(visited, pc.ProjectName)
			if pc.ProjectName == "alpha" {
				return fmt.Errorf("fetching origin: %w", fetchErr)
			}
			return nil
		},
	}

	root := &cobra.Command{Use: "anvil"}
	root.PersistentFlags().Bool("dry-run", false, "")
	root.PersistentFlags().Bool("verbose", false, "")
	root.PersistentFlags().Bool("quiet", false, "")
	root.AddCommand(newPruneCommand(deps))
	root.SetArgs([]string{"prune", "--force"})

	err := root.Execute()

	require.ErrorIs(t, err, fetchErr)
	assert.Equal(t, []string{"alpha", "beta"}, visited)
}

func TestPruneProject_ReportsRemovedOnlyAfterSuccessfulRemoval(t *testing.T) {
	failedPath := "/worktrees/feature-failed"
	successPath := "/worktrees/feature-success"
	removeErr := errors.New("git removal failed")
	deps := pruneProjectDependencies{
		fetchOrigin: func(string) error { return nil },
		listWorktrees: func(string) ([]git.Worktree, error) {
			return []git.Worktree{
				{Path: failedPath, Branch: "feature-failed"},
				{Path: successPath, Branch: "feature-success"},
			}, nil
		},
		isMerged: func(string, string, string) (bool, error) { return true, nil },
		removeLifecycle: removeLifecycleDependencies{
			readLocalState: func(string) (*config.LocalState, error) { return &config.LocalState{}, nil },
			resolvePreset:  func(*ProjectContext, string, string) presets.ResolvedPreset { return presets.ResolvedPreset{} },
			removeWorktree: func(_ string, path string, _ bool) error {
				if path == failedPath {
					return removeErr
				}
				return nil
			},
		},
	}

	output := captureStdout(t, func() {
		err := pruneProjectWithDependencies(pcForPruneTest(), true, false, false, false, false, deps)
		require.ErrorIs(t, err, removeErr)
	})

	assert.NotContains(t, output, "Removed: "+failedPath)
	assert.Contains(t, output, "Removed: "+successPath)
}

func TestPlanWorktreeCleanup_RejectsUnreadableOrInvalidStateWithoutForce(t *testing.T) {
	tests := []struct {
		name      string
		readState func(string) (*config.LocalState, error)
		want      string
	}{
		{
			name: "unreadable",
			readState: func(string) (*config.LocalState, error) {
				return nil, errors.New("permission denied")
			},
			want: "cannot read .anvil.local",
		},
		{
			name: "invalid",
			readState: func(string) (*config.LocalState, error) {
				return &config.LocalState{Databases: []config.OwnedDatabase{
					{Name: "unsafe-name!", Engine: "mysql", Role: config.DbRoleApplication},
				}}, nil
			},
			want: "invalid database records in .anvil.local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := planWorktreeCleanup("/worktrees/feature-state", false, false, false, tt.readState)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestPruneProject_ForceAndKeepDBOptionsAreShared(t *testing.T) {
	worktreePath := t.TempDir()
	require.NoError(t, config.WriteLocalState(worktreePath, config.LocalState{Databases: []config.OwnedDatabase{
		{Name: "app_feature_kept", Engine: "mysql", Role: config.DbRoleApplication},
	}}))

	client := steps.NewMockDatabaseClient()
	client.AddDatabase("app_feature_kept")
	manager := scaffold.NewScaffoldManagerWithRegistry(&removeTestRegistry{
		client: client,
		output: &bytes.Buffer{},
	})
	resolvedPreset := resolvedRemoveTestPreset()
	removeCalls := 0
	deps := pruneProjectDependencies{
		fetchOrigin: func(string) error { return nil },
		listWorktrees: func(string) ([]git.Worktree, error) {
			return []git.Worktree{{Path: worktreePath, Branch: "feature-kept"}}, nil
		},
		isMerged: func(string, string, string) (bool, error) { return true, nil },
		removeLifecycle: removeLifecycleDependencies{
			readLocalState:  config.ReadLocalState,
			scaffoldManager: func(*ProjectContext) *scaffold.ScaffoldManager { return manager },
			resolvePreset:   func(*ProjectContext, string, string) presets.ResolvedPreset { return resolvedPreset },
			removeWorktree: func(string, string, bool) error {
				removeCalls++
				return nil
			},
		},
	}

	err := pruneProjectWithDependencies(&ProjectContext{
		GitDir:        "git-dir",
		DefaultBranch: "main",
		Config:        &config.Config{Preset: "remove-test"},
	}, true, false, false, false, true, deps)

	require.NoError(t, err)
	assert.Equal(t, 1, removeCalls)
	assert.Empty(t, client.GetDropCalls(), "--keep-db must skip database cleanup")
}

func TestPruneProject_DryRunRunsCleanupWithoutGitRemoval(t *testing.T) {
	worktreePath := t.TempDir()
	require.NoError(t, config.WriteLocalState(worktreePath, config.LocalState{Databases: []config.OwnedDatabase{
		{Name: "app_feature_dry", Engine: "mysql", Role: config.DbRoleApplication},
		{Name: "app_feature_dry_test", Engine: "mysql", Role: config.DbRoleTesting},
	}}))

	client := steps.NewMockDatabaseClient()
	for _, name := range []string{
		"app_feature_dry",
		"app_feature_dry_test",
		"app_feature_dry_test_1",
		"app_feature_dry_test_test_1",
	} {
		client.AddDatabase(name)
	}
	var cleanupOutput bytes.Buffer
	manager := scaffold.NewScaffoldManagerWithRegistry(&removeTestRegistry{client: client, output: &cleanupOutput})
	resolvedPreset := resolvedRemoveTestPreset()
	removeCalls := 0
	deps := pruneProjectDependencies{
		fetchOrigin: func(string) error { return nil },
		listWorktrees: func(string) ([]git.Worktree, error) {
			return []git.Worktree{{Path: worktreePath, Branch: "feature-dry"}}, nil
		},
		isMerged: func(string, string, string) (bool, error) { return true, nil },
		removeLifecycle: removeLifecycleDependencies{
			readLocalState:  config.ReadLocalState,
			scaffoldManager: func(*ProjectContext) *scaffold.ScaffoldManager { return manager },
			resolvePreset:   func(*ProjectContext, string, string) presets.ResolvedPreset { return resolvedPreset },
			removeWorktree: func(string, string, bool) error {
				removeCalls++
				return nil
			},
		},
	}

	err := pruneProjectWithDependencies(&ProjectContext{
		GitDir:        "git-dir",
		DefaultBranch: "main",
		Config:        &config.Config{Preset: "remove-test"},
	}, true, true, false, true, false, deps)

	require.NoError(t, err)
	assert.Zero(t, removeCalls)
	assert.Empty(t, client.GetDropCalls())
	assert.Equal(t, "Would drop database: app_feature_dry\n"+
		"Would drop database: app_feature_dry_test\n"+
		"Would drop database: app_feature_dry_test_1\n"+
		"Would drop database: app_feature_dry_test_test_1\n", cleanupOutput.String())
}

func TestPruneProject_FetchFailureStopsBeforeMutation(t *testing.T) {
	fetchErr := errors.New("origin unavailable")
	listCalls := 0
	mergeCalls := 0
	removeCalls := 0
	deps := pruneProjectDependencies{
		fetchOrigin: func(string) error { return fetchErr },
		listWorktrees: func(string) ([]git.Worktree, error) {
			listCalls++
			return []git.Worktree{{Path: "/worktrees/feature-stale", Branch: "feature-stale"}}, nil
		},
		isMerged: func(string, string, string) (bool, error) {
			mergeCalls++
			return true, nil
		},
		removeLifecycle: removeLifecycleDependencies{
			readLocalState: func(string) (*config.LocalState, error) { return &config.LocalState{}, nil },
			resolvePreset:  func(*ProjectContext, string, string) presets.ResolvedPreset { return presets.ResolvedPreset{} },
			removeWorktree: func(string, string, bool) error {
				removeCalls++
				return nil
			},
		},
	}

	err := pruneProjectWithDependencies(pcForPruneTest(), true, false, false, false, false, deps)

	require.ErrorIs(t, err, fetchErr)
	assert.Zero(t, listCalls)
	assert.Zero(t, mergeCalls)
	assert.Zero(t, removeCalls)
}

func TestPruneProject_SkipsBareDetachedAndLockedRecords(t *testing.T) {
	mergeCalls := 0
	removeCalls := 0
	deps := pruneProjectDependencies{
		fetchOrigin: func(string) error { return nil },
		listWorktrees: func(string) ([]git.Worktree, error) {
			return []git.Worktree{
				{Path: "/worktrees/bare", Bare: true},
				{Path: "/worktrees/detached", Detached: true},
				{Path: "/worktrees/locked", Branch: "feature-locked", Locked: true, LockReason: "keep for review"},
			}, nil
		},
		isMerged: func(string, string, string) (bool, error) {
			mergeCalls++
			return true, nil
		},
		removeLifecycle: removeLifecycleDependencies{
			readLocalState: func(string) (*config.LocalState, error) { return &config.LocalState{}, nil },
			detectPreset:   func(*ProjectContext, string) string { return "" },
			removeWorktree: func(string, string, bool) error {
				removeCalls++
				return nil
			},
		},
	}

	require.NoError(t, pruneProjectWithDependencies(pcForPruneTest(), true, false, false, false, false, deps))

	assert.Zero(t, mergeCalls, "prune must not merge-check non-attached records")
	assert.Zero(t, removeCalls, "prune must not remove non-attached or locked records")
}

func TestPruneProject_SkipsBranchlessRecord(t *testing.T) {
	mergeCalls := 0
	deps := pruneProjectDependencies{
		fetchOrigin: func(string) error { return nil },
		listWorktrees: func(string) ([]git.Worktree, error) {
			return []git.Worktree{{Path: "/worktrees/malformed"}}, nil
		},
		isMerged: func(string, string, string) (bool, error) {
			mergeCalls++
			return true, nil
		},
		removeLifecycle: removeLifecycleDependencies{
			readLocalState: func(string) (*config.LocalState, error) { return &config.LocalState{}, nil },
			detectPreset:   func(*ProjectContext, string) string { return "" },
			removeWorktree: func(string, string, bool) error { return nil },
		},
	}

	require.NoError(t, pruneProjectWithDependencies(pcForPruneTest(), true, false, false, false, false, deps))
	assert.Zero(t, mergeCalls, "branchless records must not reach merge probing")
}

func TestPruneProject_PreservesMergeCheckFailureWhenSelectionEmpty(t *testing.T) {
	mergeErr := errors.New("cannot inspect merge status")
	selectionCalls := 0
	deps := pruneProjectDependencies{
		fetchOrigin: func(string) error { return nil },
		listWorktrees: func(string) ([]git.Worktree, error) {
			return []git.Worktree{
				{Path: "/worktrees/feature-check-failed", Branch: "feature-check-failed"},
				{Path: "/worktrees/feature-selected", Branch: "feature-selected"},
			}, nil
		},
		isMerged: func(_ string, branch string, _ string) (bool, error) {
			if branch == "feature-check-failed" {
				return false, mergeErr
			}
			return true, nil
		},
		selectWorktrees: func(worktrees []git.Worktree) ([]git.Worktree, error) {
			selectionCalls++
			assert.Len(t, worktrees, 1)
			return nil, nil
		},
		removeLifecycle: removeLifecycleDependencies{
			readLocalState: func(string) (*config.LocalState, error) { return &config.LocalState{}, nil },
			resolvePreset:  func(*ProjectContext, string, string) presets.ResolvedPreset { return presets.ResolvedPreset{} },
			removeWorktree: func(string, string, bool) error { return nil },
		},
	}

	err := pruneProjectWithDependencies(pcForPruneTest(), false, false, false, false, false, deps)

	require.ErrorIs(t, err, mergeErr)
	assert.Equal(t, 1, selectionCalls)
}

func TestPruneProject_PreservesMergeCheckFailureWhenConfirmationDeclined(t *testing.T) {
	mergeErr := errors.New("cannot inspect merge status")
	confirmationCalls := 0
	deps := pruneProjectDependencies{
		fetchOrigin: func(string) error { return nil },
		listWorktrees: func(string) ([]git.Worktree, error) {
			return []git.Worktree{
				{Path: "/worktrees/feature-check-failed", Branch: "feature-check-failed"},
				{Path: "/worktrees/feature-selected", Branch: "feature-selected"},
			}, nil
		},
		isMerged: func(_ string, branch string, _ string) (bool, error) {
			if branch == "feature-check-failed" {
				return false, mergeErr
			}
			return true, nil
		},
		selectWorktrees: func(worktrees []git.Worktree) ([]git.Worktree, error) {
			assert.Len(t, worktrees, 1)
			return worktrees, nil
		},
		confirmRemoval: func(count int) (bool, error) {
			confirmationCalls++
			assert.Equal(t, 1, count)
			return false, nil
		},
		removeLifecycle: removeLifecycleDependencies{
			readLocalState: func(string) (*config.LocalState, error) { return &config.LocalState{}, nil },
			resolvePreset:  func(*ProjectContext, string, string) presets.ResolvedPreset { return presets.ResolvedPreset{} },
			removeWorktree: func(string, string, bool) error { return nil },
		},
	}

	err := pruneProjectWithDependencies(pcForPruneTest(), false, false, false, false, false, deps)

	require.ErrorIs(t, err, mergeErr)
	assert.Equal(t, 1, confirmationCalls)
}

func TestPruneProject_PreservesPriorFailureWithSelectionOrConfirmationError(t *testing.T) {
	mergeErr := errors.New("cannot inspect merge status")
	selectionErr := errors.New("selection failed")
	confirmationErr := errors.New("confirmation failed")
	tests := []struct {
		name            string
		selectWorktrees func([]git.Worktree) ([]git.Worktree, error)
		confirmRemoval  func(int) (bool, error)
		currentFailure  error
	}{
		{
			name: "selection",
			selectWorktrees: func([]git.Worktree) ([]git.Worktree, error) {
				return nil, selectionErr
			},
			currentFailure: selectionErr,
		},
		{
			name: "confirmation",
			selectWorktrees: func(worktrees []git.Worktree) ([]git.Worktree, error) {
				return worktrees, nil
			},
			confirmRemoval: func(int) (bool, error) {
				return false, confirmationErr
			},
			currentFailure: confirmationErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := pruneProjectDependencies{
				fetchOrigin: func(string) error { return nil },
				listWorktrees: func(string) ([]git.Worktree, error) {
					return []git.Worktree{
						{Path: "/worktrees/feature-check-failed", Branch: "feature-check-failed"},
						{Path: "/worktrees/feature-selected", Branch: "feature-selected"},
					}, nil
				},
				isMerged: func(_ string, branch string, _ string) (bool, error) {
					if branch == "feature-check-failed" {
						return false, mergeErr
					}
					return true, nil
				},
				selectWorktrees: tt.selectWorktrees,
				confirmRemoval:  tt.confirmRemoval,
				removeLifecycle: removeLifecycleDependencies{
					readLocalState: func(string) (*config.LocalState, error) { return &config.LocalState{}, nil },
					resolvePreset:  func(*ProjectContext, string, string) presets.ResolvedPreset { return presets.ResolvedPreset{} },
					removeWorktree: func(string, string, bool) error { return nil },
				},
			}

			err := pruneProjectWithDependencies(pcForPruneTest(), false, false, false, false, false, deps)

			require.ErrorIs(t, err, mergeErr)
			require.ErrorIs(t, err, tt.currentFailure)
		})
	}
}

func TestPruneProject_ReturnsMergeCheckFailureAfterContinuing(t *testing.T) {
	mergeErr := errors.New("cannot inspect merge status")
	removeCalls := 0
	deps := pruneProjectDependencies{
		fetchOrigin: func(string) error { return nil },
		listWorktrees: func(string) ([]git.Worktree, error) {
			return []git.Worktree{
				{Path: "/worktrees/feature-check-failed", Branch: "feature-check-failed"},
				{Path: "/worktrees/feature-check-success", Branch: "feature-check-success"},
			}, nil
		},
		isMerged: func(_ string, branch string, _ string) (bool, error) {
			if branch == "feature-check-failed" {
				return false, mergeErr
			}
			return true, nil
		},
		removeLifecycle: removeLifecycleDependencies{
			readLocalState: func(string) (*config.LocalState, error) { return &config.LocalState{}, nil },
			resolvePreset:  func(*ProjectContext, string, string) presets.ResolvedPreset { return presets.ResolvedPreset{} },
			removeWorktree: func(string, string, bool) error {
				removeCalls++
				return nil
			},
		},
	}

	err := pruneProjectWithDependencies(pcForPruneTest(), true, false, false, false, false, deps)

	require.ErrorIs(t, err, mergeErr)
	assert.Equal(t, 1, removeCalls, "prune should continue after merge-check failure")
}

func pcForPruneTest() *ProjectContext {
	return &ProjectContext{
		GitDir:        "git-dir",
		DefaultBranch: "main",
		Config:        &config.Config{},
	}
}

func captureStdout(t *testing.T, run func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	previous := os.Stdout
	os.Stdout = writer
	writerClosed := false
	readerClosed := false
	closeWriter := func() error {
		if writerClosed {
			return nil
		}
		writerClosed = true
		return writer.Close()
	}
	closeReader := func() error {
		if readerClosed {
			return nil
		}
		readerClosed = true
		return reader.Close()
	}
	defer func() {
		os.Stdout = previous
		if err := closeWriter(); err != nil {
			t.Errorf("closing captured stdout writer: %v", err)
		}
		if err := closeReader(); err != nil {
			t.Errorf("closing captured stdout reader: %v", err)
		}
	}()

	run()
	require.NoError(t, closeWriter())
	output, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, closeReader())
	return string(output)
}
