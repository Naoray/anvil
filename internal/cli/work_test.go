package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/artisanexperiences/arbor/internal/git"
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

	// Clone to bare repo and set up like arbor init does
	projectDir := t.TempDir()
	barePath := filepath.Join(projectDir, ".bare")
	cmd = exec.Command("git", "clone", "--bare", sourceDir, barePath)
	requireNoError(t, cmd.Run())

	// Configure fetch refspec
	requireNoError(t, git.ConfigureFetchRefspec(barePath, sourceDir))

	// Create main worktree
	mainPath := filepath.Join(projectDir, "main")
	requireNoError(t, git.CreateWorktree(barePath, mainPath, "main", ""))

	// Set up tracking for main (this is what would happen in work command)
	requireNoError(t, git.SetBranchUpstream(barePath, "main", "origin"))

	// Verify main has tracking
	hasTracking, err := git.HasBranchTracking(barePath, "main")
	assert.NoError(t, err)
	assert.True(t, hasTracking)

	// Create feature branch worktree
	featurePath := filepath.Join(projectDir, "feature")
	requireNoError(t, git.CreateWorktree(barePath, featurePath, "feature", "main"))

	// Set up tracking for feature (this is what would happen in work command)
	requireNoError(t, git.SetBranchUpstream(barePath, "feature", "origin"))

	// Verify feature has tracking
	hasTracking, err = git.HasBranchTracking(barePath, "feature")
	assert.NoError(t, err)
	assert.True(t, hasTracking)

	// Check the tracking config
	cmd = exec.Command("git", "-C", barePath, "config", "--get", "branch.feature.remote")
	output, err := cmd.Output()
	assert.NoError(t, err)
	assert.Equal(t, "origin", strings.TrimSpace(string(output)))

	cmd = exec.Command("git", "-C", barePath, "config", "--get", "branch.feature.merge")
	output, err = cmd.Output()
	assert.NoError(t, err)
	assert.Equal(t, "refs/heads/feature", strings.TrimSpace(string(output)))
}
