package git

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfigureFetchRefspec(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")

	remoteURL := "git@github.com:test/repo.git"
	err := ConfigureFetchRefspec(gitDir, remoteURL)
	assert.NoError(t, err)

	// Verify remote.origin.url is set
	cmd := exec.Command("git", "-C", repoDir, "config", "--get", "remote.origin.url")
	output, err := cmd.Output()
	assert.NoError(t, err)
	assert.Equal(t, remoteURL, strings.TrimSpace(string(output)))

	// Verify fetch refspec is set
	cmd = exec.Command("git", "-C", repoDir, "config", "--get", "remote.origin.fetch")
	output, err = cmd.Output()
	assert.NoError(t, err)
	assert.Equal(t, "+refs/heads/*:refs/remotes/origin/*", strings.TrimSpace(string(output)))
}

func TestConfigureFetchRefspec_Idempotent(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")

	remoteURL := "git@github.com:test/repo.git"

	// Configure first time
	err := ConfigureFetchRefspec(gitDir, remoteURL)
	assert.NoError(t, err)

	// Configure second time - should not error
	err = ConfigureFetchRefspec(gitDir, remoteURL)
	assert.NoError(t, err)

	// Verify still correct
	url, err := GetRemoteURL(gitDir, "origin")
	assert.NoError(t, err)
	assert.Equal(t, remoteURL, url)
}

func TestGetRemoteURL(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")

	// No remote configured initially
	url, err := GetRemoteURL(gitDir, "origin")
	assert.NoError(t, err)
	assert.Equal(t, "", url)

	// Configure it
	remoteURL := "git@github.com:test/repo.git"
	err = ConfigureFetchRefspec(gitDir, remoteURL)
	assert.NoError(t, err)

	// Now should be set
	url, err = GetRemoteURL(gitDir, "origin")
	assert.NoError(t, err)
	assert.Equal(t, remoteURL, url)
}

func TestGetRemoteURL_NotConfigured(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")

	// Remote not configured - should return empty string, not error
	url, err := GetRemoteURL(gitDir, "origin")
	assert.NoError(t, err)
	assert.Equal(t, "", url)
}

func TestGetRemoteURLFromWorktree(t *testing.T) {
	repoDir := createTestRepo(t)

	// Set remote on the repo
	cmd := exec.Command("git", "remote", "add", "origin", "git@github.com:test/repo.git")
	cmd.Dir = repoDir
	err := cmd.Run()
	assert.NoError(t, err)

	// Get remote URL from the repo
	url, err := GetRemoteURLFromWorktree(repoDir)
	assert.NoError(t, err)
	assert.Equal(t, "git@github.com:test/repo.git", url)
}

func TestGetRemoteURLFromWorktree_NotConfigured(t *testing.T) {
	repoDir := createTestRepo(t)

	// No remote configured - should error
	_, err := GetRemoteURLFromWorktree(repoDir)
	assert.Error(t, err)
}

func TestHasFetchRefspec(t *testing.T) {
	repoDir := createTestRepo(t)
	gitDir := filepath.Join(repoDir, ".git")

	// Initially not configured
	has, err := HasFetchRefspec(gitDir)
	assert.NoError(t, err)
	assert.False(t, has)

	// Configure it
	err = ConfigureFetchRefspec(gitDir, "git@github.com:test/repo.git")
	assert.NoError(t, err)

	// Now should be set
	has, err = HasFetchRefspec(gitDir)
	assert.NoError(t, err)
	assert.True(t, has)
}
