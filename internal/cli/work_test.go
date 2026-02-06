package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/naoray/anvil/internal/git"
)

func TestWorkCommand_SetsUpBranchTracking(t *testing.T) {
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

	// Create initial commit
	readmePath := filepath.Join(sourceDir, "README.md")
	requireNoError(t, os.WriteFile(readmePath, []byte("test"), 0644))

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = sourceDir
	requireNoError(t, cmd.Run())

	// Use the source repo's .git as gitDir
	gitDir := filepath.Join(sourceDir, ".git")
	parentDir := filepath.Dir(sourceDir)

	// Add a remote so tracking can be set
	cmd = exec.Command("git", "-C", sourceDir, "remote", "add", "origin", sourceDir)
	requireNoError(t, cmd.Run())

	// Configure fetch refspec
	requireNoError(t, git.ConfigureFetchRefspec(gitDir, sourceDir))

	detachHEAD(t, sourceDir)

	// Create main worktree
	mainPath := filepath.Join(parentDir, "main-wt")
	requireNoError(t, git.CreateWorktree(gitDir, mainPath, "main", ""))

	// Set up tracking for main
	requireNoError(t, git.SetBranchUpstream(gitDir, "main", "origin"))

	// Verify main has tracking
	hasTracking, err := git.HasBranchTracking(gitDir, "main")
	assert.NoError(t, err)
	assert.True(t, hasTracking)

	// Create feature branch worktree
	featurePath := filepath.Join(parentDir, "feature-wt")
	requireNoError(t, git.CreateWorktree(gitDir, featurePath, "feature", "main"))

	// Set up tracking for feature
	requireNoError(t, git.SetBranchUpstream(gitDir, "feature", "origin"))

	// Verify feature has tracking
	hasTracking, err = git.HasBranchTracking(gitDir, "feature")
	assert.NoError(t, err)
	assert.True(t, hasTracking)

	// Check the tracking config
	cmd = exec.Command("git", "-C", sourceDir, "config", "--get", "branch.feature.remote")
	output, err := cmd.Output()
	assert.NoError(t, err)
	assert.Equal(t, "origin", strings.TrimSpace(string(output)))

	cmd = exec.Command("git", "-C", sourceDir, "config", "--get", "branch.feature.merge")
	output, err = cmd.Output()
	assert.NoError(t, err)
	assert.Equal(t, "refs/heads/feature", strings.TrimSpace(string(output)))
}
