package git

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfigureFetchRefspec(t *testing.T) {
	barePath, _ := createTestRepo(t)

	remoteURL := "git@github.com:test/repo.git"
	err := ConfigureFetchRefspec(barePath, remoteURL)
	assert.NoError(t, err)

	// Verify remote.origin.url is set
	cmd := exec.Command("git", "-C", barePath, "config", "--get", "remote.origin.url")
	output, err := cmd.Output()
	assert.NoError(t, err)
	assert.Equal(t, remoteURL, strings.TrimSpace(string(output)))

	// Verify fetch refspec is set
	cmd = exec.Command("git", "-C", barePath, "config", "--get", "remote.origin.fetch")
	output, err = cmd.Output()
	assert.NoError(t, err)
	assert.Equal(t, "+refs/heads/*:refs/remotes/origin/*", strings.TrimSpace(string(output)))
}

func TestConfigureFetchRefspec_Idempotent(t *testing.T) {
	barePath, _ := createTestRepo(t)

	remoteURL := "git@github.com:test/repo.git"

	// Configure first time
	err := ConfigureFetchRefspec(barePath, remoteURL)
	assert.NoError(t, err)

	// Configure second time - should not error
	err = ConfigureFetchRefspec(barePath, remoteURL)
	assert.NoError(t, err)

	// Verify still correct
	url, err := GetRemoteURL(barePath, "origin")
	assert.NoError(t, err)
	assert.Equal(t, remoteURL, url)
}

func TestGetRemoteURL(t *testing.T) {
	barePath, _ := createTestRepo(t)

	// Bare repos from clone --bare have remote.origin.url set to source
	// Remove it first to test the "not configured" case
	cmd := exec.Command("git", "-C", barePath, "config", "--unset", "remote.origin.url")
	cmd.Run() // Ignore error if it doesn't exist

	// Initially not configured
	url, err := GetRemoteURL(barePath, "origin")
	assert.NoError(t, err)
	assert.Equal(t, "", url)

	// Configure it
	remoteURL := "git@github.com:test/repo.git"
	err = ConfigureFetchRefspec(barePath, remoteURL)
	assert.NoError(t, err)

	// Now should be set
	url, err = GetRemoteURL(barePath, "origin")
	assert.NoError(t, err)
	assert.Equal(t, remoteURL, url)
}

func TestGetRemoteURL_NotConfigured(t *testing.T) {
	barePath, _ := createTestRepo(t)

	// Bare repos from clone --bare have remote.origin.url set to source
	// Remove it first to test the "not configured" case
	cmd := exec.Command("git", "-C", barePath, "config", "--unset", "remote.origin.url")
	cmd.Run() // Ignore error if it doesn't exist

	// Remote not configured - should return empty string, not error
	url, err := GetRemoteURL(barePath, "origin")
	assert.NoError(t, err)
	assert.Equal(t, "", url)
}

func TestGetRemoteURLFromWorktree(t *testing.T) {
	_, repoDir := createTestRepo(t)

	// Set remote on the original repo
	cmd := exec.Command("git", "remote", "add", "origin", "git@github.com:test/repo.git")
	cmd.Dir = repoDir
	err := cmd.Run()
	assert.NoError(t, err)

	// Get remote URL from the original repo (acting as worktree)
	url, err := GetRemoteURLFromWorktree(repoDir)
	assert.NoError(t, err)
	assert.Equal(t, "git@github.com:test/repo.git", url)
}

func TestGetRemoteURLFromWorktree_NotConfigured(t *testing.T) {
	_, repoDir := createTestRepo(t)

	// No remote configured - should error
	_, err := GetRemoteURLFromWorktree(repoDir)
	assert.Error(t, err)
}

func TestHasFetchRefspec(t *testing.T) {
	barePath, _ := createTestRepo(t)

	// Initially not configured
	has, err := HasFetchRefspec(barePath)
	assert.NoError(t, err)
	assert.False(t, has)

	// Configure it
	err = ConfigureFetchRefspec(barePath, "git@github.com:test/repo.git")
	assert.NoError(t, err)

	// Now should be set
	has, err = HasFetchRefspec(barePath)
	assert.NoError(t, err)
	assert.True(t, has)
}
