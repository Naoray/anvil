package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getAnvilBinary(t *testing.T) string {
	t.Helper()

	binary := os.Getenv("ANVIL_TEST_BIN")
	if binary == "" {
		t.Fatal("ANVIL_TEST_BIN is not set; TestMain must build the anvil test binary before tests run")
	}
	return binary
}

func TestScaffoldRequiresProject(t *testing.T) {
	anvilBinary := getAnvilBinary(t)
	tmpDir := t.TempDir()

	anvilCmd := exec.Command(anvilBinary, "scaffold", "main", "--dry-run")
	anvilCmd.Dir = tmpDir
	output, err := anvilCmd.CombinedOutput()
	assert.Error(t, err)
	assert.Contains(t, string(output), "opening project")
}

func TestScaffoldHelp(t *testing.T) {
	anvilBinary := getAnvilBinary(t)

	anvilCmd := exec.Command(anvilBinary, "scaffold", "--help")
	output, err := anvilCmd.CombinedOutput()
	assert.NoError(t, err)
	assert.Contains(t, string(output), "Run scaffold steps for an existing worktree")
	assert.Contains(t, string(output), "[WORKTREE]")
}

func TestScaffoldInvalidWorktree(t *testing.T) {
	anvilBinary := getAnvilBinary(t)
	tmpDir := t.TempDir()

	// Create a regular git repo
	repoDir := filepath.Join(tmpDir, "repo")
	cmd := exec.Command("git", "init", "-b", "main", repoDir)
	require.NoError(t, cmd.Run())

	anvilCmd := exec.Command(anvilBinary, "scaffold", "nonexistent", "--dry-run")
	anvilCmd.Dir = repoDir
	output, err := anvilCmd.CombinedOutput()
	assert.Error(t, err)
	// May get "opening project" or "no worktrees" error depending on whether it's linked
	assert.True(t, len(output) > 0)
}

func TestScaffoldNoWorktreesInProject(t *testing.T) {
	anvilBinary := getAnvilBinary(t)
	tmpDir := t.TempDir()

	// Create a regular git repo
	repoDir := filepath.Join(tmpDir, "repo")
	cmd := exec.Command("git", "init", "-b", "main", repoDir)
	require.NoError(t, cmd.Run())

	anvilCmd := exec.Command(anvilBinary, "scaffold", "--dry-run", "--no-interactive")
	anvilCmd.Dir = repoDir
	output, err := anvilCmd.CombinedOutput()
	assert.Error(t, err)
	// May get "opening project" or "no worktrees" error
	assert.True(t, len(output) > 0)
}
