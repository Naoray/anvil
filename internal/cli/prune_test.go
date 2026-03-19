package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/git"
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

// helper used by makeTestProject — reuses the one in remove_test.go but
// accepts the repo path as first arg via "-C"
func init() {
	// runGitCmd is already defined in remove_test.go (same package)
	// no redeclaration needed
}

func runGitCmdOut(dir string, args ...string) error {
	allArgs := append([]string{"-C", dir}, args...)
	return exec.Command("git", allArgs...).Run()
}
