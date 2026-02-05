package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/michaeldyrynda/arbor/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCdCmd_FindsWorktreeByFolderName(t *testing.T) {
	// Setup: create a bare repo with worktrees
	barePath, _ := createTestWorktree(t)
	projectDir := filepath.Dir(barePath)

	// Create a feature worktree
	featurePath := filepath.Join(projectDir, "feature-test")
	cmd := exec.Command("git", "worktree", "add", "-b", "feature/test", featurePath, "main")
	cmd.Dir = barePath
	require.NoError(t, cmd.Run())

	// Test: find worktree by folder name
	path, err := findWorktreePath(projectDir, barePath, "feature-test", false, nil)

	assert.NoError(t, err)
	assert.Equal(t, evalSymlinks(featurePath), evalSymlinks(path))
}

func TestCdCmd_FindsWorktreeByBranchName(t *testing.T) {
	// Setup: create a bare repo with worktrees
	barePath, _ := createTestWorktree(t)
	projectDir := filepath.Dir(barePath)

	// Create a feature worktree
	featurePath := filepath.Join(projectDir, "my-feature-folder")
	cmd := exec.Command("git", "worktree", "add", "-b", "feature/awesome", featurePath, "main")
	cmd.Dir = barePath
	require.NoError(t, cmd.Run())

	// Test: find worktree by branch name
	path, err := findWorktreePath(projectDir, barePath, "feature/awesome", false, nil)

	assert.NoError(t, err)
	assert.Equal(t, evalSymlinks(featurePath), evalSymlinks(path))
}

func TestCdCmd_FindsWorktreeByPartialMatch(t *testing.T) {
	// Setup: create a bare repo with worktrees
	barePath, _ := createTestWorktree(t)
	projectDir := filepath.Dir(barePath)

	// Create a feature worktree
	featurePath := filepath.Join(projectDir, "feature-notifications")
	cmd := exec.Command("git", "worktree", "add", "-b", "feature/notifications", featurePath, "main")
	cmd.Dir = barePath
	require.NoError(t, cmd.Run())

	// Test: find worktree by partial match
	path, err := findWorktreePath(projectDir, barePath, "notif", false, nil)

	assert.NoError(t, err)
	assert.Equal(t, evalSymlinks(featurePath), evalSymlinks(path))
}

func TestCdCmd_ReturnsErrorForNoMatch(t *testing.T) {
	// Setup: create a bare repo with worktrees
	barePath, _ := createTestWorktree(t)
	projectDir := filepath.Dir(barePath)

	// Test: no matching worktree
	_, err := findWorktreePath(projectDir, barePath, "nonexistent", false, nil)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no worktree found")
}

func TestCdCmd_ReturnsErrorForAmbiguousMatch(t *testing.T) {
	// Setup: create a bare repo with worktrees
	barePath, _ := createTestWorktree(t)
	projectDir := filepath.Dir(barePath)

	// Create multiple feature worktrees with similar names
	feature1Path := filepath.Join(projectDir, "feature-auth")
	cmd := exec.Command("git", "worktree", "add", "-b", "feature/auth", feature1Path, "main")
	cmd.Dir = barePath
	require.NoError(t, cmd.Run())

	feature2Path := filepath.Join(projectDir, "feature-auth-2fa")
	cmd = exec.Command("git", "worktree", "add", "-b", "feature/auth-2fa", feature2Path, "main")
	cmd.Dir = barePath
	require.NoError(t, cmd.Run())

	// Test: ambiguous match should return error
	_, err := findWorktreePath(projectDir, barePath, "auth", false, nil)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "multiple worktrees match")
}

func TestCdCmd_LinkedProject_FindsWorktree(t *testing.T) {
	// Setup: create a linked project structure
	repoDir := createLinkedProject(t)
	tmpDir := filepath.Dir(repoDir)
	worktreeBase := filepath.Join(tmpDir, "worktrees")
	projectWorktreeDir := filepath.Join(worktreeBase, "my-project")

	// Create worktree directory structure
	require.NoError(t, os.MkdirAll(projectWorktreeDir, 0755))

	// Create a feature worktree using git
	gitDir := filepath.Join(repoDir, ".git")
	featurePath := filepath.Join(projectWorktreeDir, "feature-test")
	cmd := exec.Command("git", "-C", repoDir, "worktree", "add", "-b", "feature/test", featurePath, "main")
	require.NoError(t, cmd.Run())

	// Create global config
	globalCfg := &config.GlobalConfig{
		WorktreeBase: worktreeBase,
		Projects: map[string]*config.ProjectInfo{
			"my-project": {
				Path:          repoDir,
				DefaultBranch: "main",
			},
		},
	}

	// Test: find worktree in linked project
	path, err := findWorktreePath(repoDir, gitDir, "feature-test", true, globalCfg)

	assert.NoError(t, err)
	assert.Equal(t, evalSymlinks(featurePath), evalSymlinks(path))
}

func TestCdCmd_OutputsPath(t *testing.T) {
	// Setup: create a bare repo with worktrees
	barePath, _ := createTestWorktree(t)
	projectDir := filepath.Dir(barePath)

	// Create a feature worktree
	featurePath := filepath.Join(projectDir, "feature-test")
	cmd := exec.Command("git", "worktree", "add", "-b", "feature/test", featurePath, "main")
	cmd.Dir = barePath
	require.NoError(t, cmd.Run())

	// Test: formatCdOutput returns correct shell command
	output := formatCdOutput(featurePath, false)
	assert.Equal(t, featurePath, output)

	// Test: with shell format
	shellOutput := formatCdOutput(featurePath, true)
	assert.Equal(t, "cd "+featurePath, shellOutput)
}

func TestCdCmd_ListsWorktreesWhenNoArg(t *testing.T) {
	// Setup: create a bare repo with worktrees
	barePath, _ := createTestWorktree(t)
	projectDir := filepath.Dir(barePath)

	// Create feature worktrees
	feature1Path := filepath.Join(projectDir, "feature-one")
	cmd := exec.Command("git", "worktree", "add", "-b", "feature/one", feature1Path, "main")
	cmd.Dir = barePath
	require.NoError(t, cmd.Run())

	feature2Path := filepath.Join(projectDir, "feature-two")
	cmd = exec.Command("git", "worktree", "add", "-b", "feature/two", feature2Path, "main")
	cmd.Dir = barePath
	require.NoError(t, cmd.Run())

	// Test: listWorktreesForCd returns worktree names
	var buf bytes.Buffer
	err := listWorktreesForCd(&buf, barePath, false)

	assert.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "feature-one")
	assert.Contains(t, output, "feature-two")
}
