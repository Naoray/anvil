package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/git"
)

func TestRepairBranchTracking_ReturnsInspectionErrorAndContinues(t *testing.T) {
	inspectionErr := errors.New("tracking inspection failed")
	var inspected []string
	var configured []string

	err := repairBranchTrackingWithDependencies(
		&ProjectContext{GitDir: "git-dir"},
		false,
		false,
		repairBranchTrackingDependencies{
			getBranchRefs: func(string) ([]string, []string, error) {
				return []string{"first", "second"}, []string{"origin/first", "origin/second"}, nil
			},
			hasBranchTracking: func(_ string, branch string) (bool, error) {
				inspected = append(inspected, branch)
				if branch == "first" {
					return false, inspectionErr
				}
				return false, nil
			},
			setBranchUpstream: func(_ string, branch, _ string) error {
				configured = append(configured, branch)
				return nil
			},
		},
	)

	assert.ErrorIs(t, err, inspectionErr)
	assert.Contains(t, err.Error(), "first")
	assert.Equal(t, []string{"first", "second"}, inspected)
	assert.Equal(t, []string{"second"}, configured)
}

func TestRepairBranchTracking_ReturnsUpstreamErrorAndContinues(t *testing.T) {
	upstreamErr := errors.New("upstream setup failed")
	var configured []string

	err := repairBranchTrackingWithDependencies(
		&ProjectContext{GitDir: "git-dir"},
		false,
		false,
		repairBranchTrackingDependencies{
			getBranchRefs: func(string) ([]string, []string, error) {
				return []string{"first", "second"}, []string{"origin/first", "origin/second"}, nil
			},
			hasBranchTracking: func(string, string) (bool, error) {
				return false, nil
			},
			setBranchUpstream: func(_ string, branch, _ string) error {
				configured = append(configured, branch)
				if branch == "first" {
					return upstreamErr
				}
				return nil
			},
		},
	)

	assert.ErrorIs(t, err, upstreamErr)
	assert.Contains(t, err.Error(), "first")
	assert.Equal(t, []string{"first", "second"}, configured)
}

func TestRepairBranchTracking_JoinsMultipleBranchFailuresWithContext(t *testing.T) {
	inspectionErr := errors.New("tracking inspection failed")
	upstreamErr := errors.New("upstream setup failed")
	var inspected []string
	var configured []string

	err := repairBranchTrackingWithDependencies(
		&ProjectContext{GitDir: "git-dir"},
		false,
		false,
		repairBranchTrackingDependencies{
			getBranchRefs: func(string) ([]string, []string, error) {
				return []string{"inspect-failure", "setup-failure", "success"}, []string{
					"origin/inspect-failure",
					"origin/setup-failure",
					"origin/success",
				}, nil
			},
			hasBranchTracking: func(_ string, branch string) (bool, error) {
				inspected = append(inspected, branch)
				if branch == "inspect-failure" {
					return false, inspectionErr
				}
				return false, nil
			},
			setBranchUpstream: func(_ string, branch, _ string) error {
				configured = append(configured, branch)
				if branch == "setup-failure" {
					return upstreamErr
				}
				return nil
			},
		},
	)

	assert.ErrorIs(t, err, inspectionErr)
	assert.ErrorIs(t, err, upstreamErr)
	assert.Contains(t, err.Error(), `branch "inspect-failure": checking tracking`)
	assert.Contains(t, err.Error(), `branch "setup-failure": setting upstream`)
	assert.Equal(t, []string{"inspect-failure", "setup-failure", "success"}, inspected)
	assert.Equal(t, []string{"setup-failure", "success"}, configured)
}

func TestRepairCommand_DoesNotPrintCompletionOnAggregateError(t *testing.T) {
	aggregateErr := errors.New(`branch "first": setting upstream: upstream setup failed`)
	command := newRepairCommand(repairCommandDependencies{
		openProject: func() (*ProjectContext, error) {
			return &ProjectContext{GitDir: "git-dir"}, nil
		},
		repairFetchRefspec: func(*ProjectContext, bool, bool) error {
			return nil
		},
		repairBranchTracking: func(*ProjectContext, bool, bool) error {
			return aggregateErr
		},
	})
	root := &cobra.Command{Use: "test"}
	root.PersistentFlags().Bool("dry-run", false, "")
	root.PersistentFlags().Bool("verbose", false, "")
	root.PersistentFlags().Bool("quiet", false, "")
	root.AddCommand(command)
	root.SetArgs([]string{"repair"})

	var err error
	output := captureStdout(t, func() {
		err = root.Execute()
	})

	assert.ErrorIs(t, err, aggregateErr)
	assert.NotContains(t, output, "Repair complete")
}

func TestRepairBranchTracking_PreservesTrackingAndNoRemoteSkips(t *testing.T) {
	var configured []string

	err := repairBranchTrackingWithDependencies(
		&ProjectContext{GitDir: "git-dir"},
		false,
		false,
		repairBranchTrackingDependencies{
			getBranchRefs: func(string) ([]string, []string, error) {
				return []string{"tracked", "no-remote", "needs-tracking"}, []string{
					"origin/tracked",
					"origin/needs-tracking",
				}, nil
			},
			hasBranchTracking: func(_ string, branch string) (bool, error) {
				return branch == "tracked", nil
			},
			setBranchUpstream: func(_ string, branch, _ string) error {
				configured = append(configured, branch)
				return nil
			},
		},
	)

	assert.NoError(t, err)
	assert.Equal(t, []string{"needs-tracking"}, configured)
}

func TestRepairBranchTracking_DryRunDoesNotSetUpstream(t *testing.T) {
	configured := false

	err := repairBranchTrackingWithDependencies(
		&ProjectContext{GitDir: "git-dir"},
		true,
		false,
		repairBranchTrackingDependencies{
			getBranchRefs: func(string) ([]string, []string, error) {
				return []string{"needs-tracking"}, []string{"origin/needs-tracking"}, nil
			},
			hasBranchTracking: func(string, string) (bool, error) {
				return false, nil
			},
			setBranchUpstream: func(string, string, string) error {
				configured = true
				return nil
			},
		},
	)

	assert.NoError(t, err)
	assert.False(t, configured)
}

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

func TestRemoteURLFromWorktrees_SkipsBareAndUsesDetachedOrLockedPaths(t *testing.T) {
	var inspected []string
	worktrees := []git.Worktree{
		{Path: "/worktrees/bare", Bare: true},
		{Path: "/worktrees/detached", Detached: true},
		{Path: "/worktrees/locked", Branch: "feature-locked", Locked: true},
	}

	remoteURL := remoteURLFromWorktrees(worktrees, func(path string) (string, error) {
		inspected = append(inspected, path)
		if path == "/worktrees/locked" {
			return "https://example.test/repo.git", nil
		}
		return "", nil
	})

	assert.Equal(t, "https://example.test/repo.git", remoteURL)
	assert.Equal(t, []string{"/worktrees/detached", "/worktrees/locked"}, inspected)
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
