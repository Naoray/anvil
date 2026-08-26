package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/scaffold"
	"github.com/naoray/anvil/internal/scaffold/steps"
)

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

	err := pruneProject(pc, true, true /* dryRun */, false, false)
	require.NoError(t, err)

	_, err = os.Stat(wtPath)
	assert.NoError(t, err, "dry-run should not remove the worktree")
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
	manager.RegisterPreset(removeTestPreset{})

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
			detectPreset:    func(*ProjectContext, string) string { return "remove-test" },
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
