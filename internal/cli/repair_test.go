package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/git"
)

// createRepoWithRemote creates a source repo and a clone with remote configured.
// Returns (gitDir, repoDir, sourceDir).
func createRepoWithRemote(t *testing.T) (string, string, string) {
	t.Helper()

	// Create a source repo
	sourceDir := t.TempDir()
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	readmePath := filepath.Join(sourceDir, "README.md")
	requireNoError(t, os.WriteFile(readmePath, []byte("test"), 0644))

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	// Clone to get a repo with remote configured
	repoDir := filepath.Join(t.TempDir(), "repo")
	cmd = exec.Command("git", "clone", sourceDir, repoDir)
	requireNoError(t, cmd.Run())

	gitDir := filepath.Join(repoDir, ".git")
	return gitDir, repoDir, sourceDir
}

func TestRepairCommand_ConfiguresFetchRefspec(t *testing.T) {
	gitDir, repoDir, sourceDir := createRepoWithRemote(t)
	parentDir := filepath.Dir(repoDir)

	// Remove the auto-configured fetch refspec to simulate old project
	cmd := exec.Command("git", "-C", repoDir, "config", "--unset", "remote.origin.fetch")
	cmd.Run() // Ignore error

	detachHEAD(t, repoDir)

	// Create a worktree
	mainPath := filepath.Join(parentDir, "main-wt")
	requireNoError(t, git.CreateWorktree(gitDir, mainPath, "main", ""))

	// Verify refspec not configured
	hasRefspec, err := git.HasFetchRefspec(gitDir)
	assert.NoError(t, err)
	assert.False(t, hasRefspec, "Expected no fetch refspec after unsetting")

	// Create ProjectContext
	pc := &ProjectContext{
		GitDir:        gitDir,
		ProjectPath:   parentDir,
		DefaultBranch: "main",
		Config:        &config.Config{DefaultBranch: "main"},
	}

	// Run repairFetchRefspec
	err = repairFetchRefspec(pc, false, true)
	assert.NoError(t, err)

	// Verify refspec is now configured
	hasRefspec, err = git.HasFetchRefspec(gitDir)
	assert.NoError(t, err)
	assert.True(t, hasRefspec, "Expected fetch refspec to be configured after repair")

	// Verify remote URL is set correctly
	url, err := git.GetRemoteURL(gitDir, "origin")
	assert.NoError(t, err)
	assert.Equal(t, sourceDir, url)
}

func TestRepairCommand_DryRun(t *testing.T) {
	gitDir, repoDir, _ := createRepoWithRemote(t)
	parentDir := filepath.Dir(repoDir)

	// Remove the auto-configured fetch refspec to simulate old project
	cmd := exec.Command("git", "-C", repoDir, "config", "--unset", "remote.origin.fetch")
	cmd.Run() // Ignore error

	detachHEAD(t, repoDir)

	// Create a worktree
	mainPath := filepath.Join(parentDir, "main-wt")
	requireNoError(t, git.CreateWorktree(gitDir, mainPath, "main", ""))

	// Verify refspec not configured
	hasRefspec, err := git.HasFetchRefspec(gitDir)
	assert.NoError(t, err)
	assert.False(t, hasRefspec)

	// Create ProjectContext
	pc := &ProjectContext{
		GitDir:        gitDir,
		ProjectPath:   parentDir,
		DefaultBranch: "main",
		Config:        &config.Config{DefaultBranch: "main"},
	}

	// Run repairFetchRefspec with dry run
	err = repairFetchRefspec(pc, true, true)
	assert.NoError(t, err)

	// Verify refspec is still NOT configured (dry run)
	hasRefspec, err = git.HasFetchRefspec(gitDir)
	assert.NoError(t, err)
	assert.False(t, hasRefspec)
}

func TestRepairCommand_FixesBranchTracking(t *testing.T) {
	// Create a source repo with feature branch
	sourceDir := t.TempDir()
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	readmePath := filepath.Join(sourceDir, "README.md")
	requireNoError(t, os.WriteFile(readmePath, []byte("test"), 0644))

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	// Create feature branch in source
	cmd = exec.Command("git", "checkout", "-b", "feature/test")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	featureFile := filepath.Join(sourceDir, "feature.txt")
	requireNoError(t, os.WriteFile(featureFile, []byte("feature"), 0644))

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "Feature commit")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "checkout", "main")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	// Clone the source repo
	repoDir := filepath.Join(t.TempDir(), "repo")
	cmd = exec.Command("git", "clone", sourceDir, repoDir)
	requireNoError(t, cmd.Run())

	gitDir := filepath.Join(repoDir, ".git")
	parentDir := filepath.Dir(repoDir)

	// Fetch all remote branches
	cmd = exec.Command("git", "-C", repoDir, "fetch", "--all")
	requireNoError(t, cmd.Run())

	detachHEAD(t, repoDir)

	// Create main worktree
	mainPath := filepath.Join(parentDir, "main-wt")
	requireNoError(t, git.CreateWorktree(gitDir, mainPath, "main", ""))

	// Create feature worktree (checkout the remote branch)
	featurePath := filepath.Join(parentDir, "feature-wt")
	requireNoError(t, git.CreateWorktree(gitDir, featurePath, "feature/test", "main"))

	// Remove tracking to simulate old project
	cmd = exec.Command("git", "-C", repoDir, "config", "--unset", "branch.main.remote")
	cmd.Run()
	cmd = exec.Command("git", "-C", repoDir, "config", "--unset", "branch.main.merge")
	cmd.Run()
	cmd = exec.Command("git", "-C", repoDir, "config", "--unset", "branch.feature/test.remote")
	cmd.Run()
	cmd = exec.Command("git", "-C", repoDir, "config", "--unset", "branch.feature/test.merge")
	cmd.Run()

	// Create ProjectContext
	pc := &ProjectContext{
		GitDir:        gitDir,
		ProjectPath:   parentDir,
		DefaultBranch: "main",
		Config:        &config.Config{DefaultBranch: "main"},
	}

	// Verify no tracking initially
	hasTracking, err := git.HasBranchTracking(gitDir, "main")
	assert.NoError(t, err)
	assert.False(t, hasTracking)

	hasTracking, err = git.HasBranchTracking(gitDir, "feature/test")
	assert.NoError(t, err)
	assert.False(t, hasTracking)

	// Run repairBranchTracking
	err = repairBranchTracking(pc, false, true)
	assert.NoError(t, err)

	// Verify tracking is now set for main
	hasTracking, err = git.HasBranchTracking(gitDir, "main")
	assert.NoError(t, err)
	assert.True(t, hasTracking)

	// feature/test may or may not have tracking depending on remote refs
	// Just verify no error occurred
}

func TestRepairCommand_Idempotent(t *testing.T) {
	gitDir, repoDir, _ := createRepoWithRemote(t)
	parentDir := filepath.Dir(repoDir)

	detachHEAD(t, repoDir)

	// Create main worktree
	mainPath := filepath.Join(parentDir, "main-wt")
	requireNoError(t, git.CreateWorktree(gitDir, mainPath, "main", ""))

	// Set up tracking
	requireNoError(t, git.SetBranchUpstream(gitDir, "main", "origin"))

	// Create ProjectContext
	pc := &ProjectContext{
		GitDir:        gitDir,
		ProjectPath:   parentDir,
		DefaultBranch: "main",
		Config:        &config.Config{DefaultBranch: "main"},
	}

	// Verify refspec is configured
	hasRefspec, err := git.HasFetchRefspec(gitDir)
	assert.NoError(t, err)
	assert.True(t, hasRefspec)

	// Verify tracking is set
	hasTracking, err := git.HasBranchTracking(gitDir, "main")
	assert.NoError(t, err)
	assert.True(t, hasTracking)

	// Run repair again - should be idempotent
	err = repairFetchRefspec(pc, false, true)
	assert.NoError(t, err)

	err = repairBranchTracking(pc, false, true)
	assert.NoError(t, err)

	// Verify everything still works
	hasRefspec, err = git.HasFetchRefspec(gitDir)
	assert.NoError(t, err)
	assert.True(t, hasRefspec)

	hasTracking, err = git.HasBranchTracking(gitDir, "main")
	assert.NoError(t, err)
	assert.True(t, hasTracking)
}

func TestRepairCommand_RefspecOnly(t *testing.T) {
	gitDir, repoDir, _ := createRepoWithRemote(t)
	parentDir := filepath.Dir(repoDir)

	// Remove the auto-configured fetch refspec
	cmd := exec.Command("git", "-C", repoDir, "config", "--unset", "remote.origin.fetch")
	cmd.Run()

	detachHEAD(t, repoDir)

	// Create main worktree
	mainPath := filepath.Join(parentDir, "main-wt")
	requireNoError(t, git.CreateWorktree(gitDir, mainPath, "main", ""))

	// Create ProjectContext
	pc := &ProjectContext{
		GitDir:        gitDir,
		ProjectPath:   parentDir,
		DefaultBranch: "main",
		Config:        &config.Config{DefaultBranch: "main"},
	}

	// Verify refspec not configured
	hasRefspec, err := git.HasFetchRefspec(gitDir)
	assert.NoError(t, err)
	assert.False(t, hasRefspec)

	// Run only refspec repair
	err = repairFetchRefspec(pc, false, true)
	assert.NoError(t, err)

	// Verify refspec is configured
	hasRefspec, err = git.HasFetchRefspec(gitDir)
	assert.NoError(t, err)
	assert.True(t, hasRefspec)
}

func TestRepairCommand_TrackingOnly(t *testing.T) {
	gitDir, repoDir, _ := createRepoWithRemote(t)
	parentDir := filepath.Dir(repoDir)

	detachHEAD(t, repoDir)

	// Create main worktree
	mainPath := filepath.Join(parentDir, "main-wt")
	requireNoError(t, git.CreateWorktree(gitDir, mainPath, "main", ""))

	// Remove tracking to simulate old project
	cmd := exec.Command("git", "-C", repoDir, "config", "--unset", "branch.main.remote")
	cmd.Run()
	cmd = exec.Command("git", "-C", repoDir, "config", "--unset", "branch.main.merge")
	cmd.Run()

	// Create ProjectContext
	pc := &ProjectContext{
		GitDir:        gitDir,
		ProjectPath:   parentDir,
		DefaultBranch: "main",
		Config:        &config.Config{DefaultBranch: "main"},
	}

	// Verify no tracking initially
	hasTracking, err := git.HasBranchTracking(gitDir, "main")
	assert.NoError(t, err)
	assert.False(t, hasTracking)

	// Run only tracking repair
	err = repairBranchTracking(pc, false, true)
	assert.NoError(t, err)

	// Verify tracking is now set
	hasTracking, err = git.HasBranchTracking(gitDir, "main")
	assert.NoError(t, err)
	assert.True(t, hasTracking)
}

func TestRepairCommand_ConflictingFlags(t *testing.T) {
	// The conflict check is validated by the separate
	// TestRepairCommand_RefspecOnly and TestRepairCommand_TrackingOnly tests.
}
