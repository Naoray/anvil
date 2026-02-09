package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestRepoForInfo creates a regular git repo and returns the .git dir path
func createTestRepoForInfo(t *testing.T) string {
	t.Helper()
	repoDir := t.TempDir()

	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("test"), 0644))

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	return filepath.Join(repoDir, ".git")
}

func TestInfoCmd_FindsWorktreeByFolderName(t *testing.T) {
	gitDir := createTestRepoForInfo(t)
	repoDir := filepath.Dir(gitDir)
	projectDir := filepath.Dir(repoDir)

	// Create a feature worktree
	featurePath := filepath.Join(projectDir, "feature-test")
	cmd := exec.Command("git", "worktree", "add", "-b", "feature/test", featurePath, "main")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	// Test: find worktree by folder name
	path, err := findWorktreePath(gitDir, "feature-test")

	assert.NoError(t, err)
	assert.Equal(t, evalSymlinks(featurePath), evalSymlinks(path))
}

func TestInfoCmd_FindsWorktreeByBranchName(t *testing.T) {
	gitDir := createTestRepoForInfo(t)
	repoDir := filepath.Dir(gitDir)
	projectDir := filepath.Dir(repoDir)

	// Create a feature worktree
	featurePath := filepath.Join(projectDir, "my-feature-folder")
	cmd := exec.Command("git", "worktree", "add", "-b", "feature/awesome", featurePath, "main")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	// Test: find worktree by branch name
	path, err := findWorktreePath(gitDir, "feature/awesome")

	assert.NoError(t, err)
	assert.Equal(t, evalSymlinks(featurePath), evalSymlinks(path))
}

func TestInfoCmd_FindsWorktreeByPartialMatch(t *testing.T) {
	gitDir := createTestRepoForInfo(t)
	repoDir := filepath.Dir(gitDir)
	projectDir := filepath.Dir(repoDir)

	// Create a feature worktree
	featurePath := filepath.Join(projectDir, "feature-notifications")
	cmd := exec.Command("git", "worktree", "add", "-b", "feature/notifications", featurePath, "main")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	// Test: find worktree by partial match
	path, err := findWorktreePath(gitDir, "notif")

	assert.NoError(t, err)
	assert.Equal(t, evalSymlinks(featurePath), evalSymlinks(path))
}

func TestInfoCmd_ReturnsErrorForNoMatch(t *testing.T) {
	gitDir := createTestRepoForInfo(t)

	// Test: no matching worktree
	_, err := findWorktreePath(gitDir, "nonexistent")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no worktree found")
}

func TestInfoCmd_ReturnsErrorForAmbiguousMatch(t *testing.T) {
	gitDir := createTestRepoForInfo(t)
	repoDir := filepath.Dir(gitDir)
	projectDir := filepath.Dir(repoDir)

	// Create multiple feature worktrees with similar names
	feature1Path := filepath.Join(projectDir, "feature-auth")
	cmd := exec.Command("git", "worktree", "add", "-b", "feature/auth", feature1Path, "main")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	feature2Path := filepath.Join(projectDir, "feature-auth-2fa")
	cmd = exec.Command("git", "worktree", "add", "-b", "feature/auth-2fa", feature2Path, "main")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	// Test: ambiguous match should return error
	_, err := findWorktreePath(gitDir, "auth")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiple worktrees match")
}

func TestInfoCmd_OutputsPath(t *testing.T) {
	gitDir := createTestRepoForInfo(t)
	repoDir := filepath.Dir(gitDir)
	projectDir := filepath.Dir(repoDir)

	// Create a feature worktree
	featurePath := filepath.Join(projectDir, "feature-test")
	cmd := exec.Command("git", "worktree", "add", "-b", "feature/test", featurePath, "main")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	path, err := findWorktreePath(gitDir, "feature-test")
	assert.NoError(t, err)
	assert.Equal(t, evalSymlinks(featurePath), evalSymlinks(path))
}

func TestInfoCmd_ListsWorktreesWhenNoArg(t *testing.T) {
	gitDir := createTestRepoForInfo(t)
	repoDir := filepath.Dir(gitDir)
	projectDir := filepath.Dir(repoDir)

	// Create feature worktrees
	feature1Path := filepath.Join(projectDir, "feature-one")
	cmd := exec.Command("git", "worktree", "add", "-b", "feature/one", feature1Path, "main")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	feature2Path := filepath.Join(projectDir, "feature-two")
	cmd = exec.Command("git", "worktree", "add", "-b", "feature/two", feature2Path, "main")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	// Test: listWorktreesForInfo returns worktree names
	var buf bytes.Buffer
	err := listWorktreesForInfo(&buf, gitDir)

	assert.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "feature-one")
	assert.Contains(t, output, "feature-two")
}
