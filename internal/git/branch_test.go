package git

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetBranchUpstream(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")

	// Set up remote origin first
	err := ConfigureFetchRefspec(gitDir, "git@github.com:test/repo.git")
	assert.NoError(t, err)

	// Set up tracking for main branch
	err = SetBranchUpstream(gitDir, "main", "origin")
	assert.NoError(t, err)

	// Verify remote is set
	cmd := exec.Command("git", "-C", repoDir, "config", "--get", "branch.main.remote")
	output, err := cmd.Output()
	assert.NoError(t, err)
	assert.Equal(t, "origin", strings.TrimSpace(string(output)))

	// Verify merge is set
	cmd = exec.Command("git", "-C", repoDir, "config", "--get", "branch.main.merge")
	output, err = cmd.Output()
	assert.NoError(t, err)
	assert.Equal(t, "refs/heads/main", strings.TrimSpace(string(output)))
}

func TestSetBranchUpstream_Idempotent(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")

	// Set up remote origin
	err := ConfigureFetchRefspec(gitDir, "git@github.com:test/repo.git")
	assert.NoError(t, err)

	// Set up tracking first time
	err = SetBranchUpstream(gitDir, "main", "origin")
	assert.NoError(t, err)

	// Set up tracking second time - should not error
	err = SetBranchUpstream(gitDir, "main", "origin")
	assert.NoError(t, err)

	// Verify still correct
	hasTracking, err := HasBranchTracking(gitDir, "main")
	assert.NoError(t, err)
	assert.True(t, hasTracking)
}

func TestHasBranchTracking(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")

	// Initially no tracking
	has, err := HasBranchTracking(gitDir, "main")
	assert.NoError(t, err)
	assert.False(t, has)

	// Set up remote origin and tracking
	err = ConfigureFetchRefspec(gitDir, "git@github.com:test/repo.git")
	assert.NoError(t, err)

	err = SetBranchUpstream(gitDir, "main", "origin")
	assert.NoError(t, err)

	// Now should have tracking
	has, err = HasBranchTracking(gitDir, "main")
	assert.NoError(t, err)
	assert.True(t, has)
}

func TestGetBranchRefs(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")

	// Get branches
	local, remote, err := GetBranchRefs(gitDir)
	assert.NoError(t, err)

	// Should have at least main branch
	assert.Contains(t, local, "main")
	// No remotes configured yet
	assert.Empty(t, remote)
}

func TestListLocalBranches(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")

	// Get local branches
	branches, err := ListLocalBranches(gitDir)
	assert.NoError(t, err)

	// Should have at least main branch
	assert.Contains(t, branches, "main")
}
