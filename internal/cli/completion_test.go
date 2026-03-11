package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompleteWorktreeNames_ReturnsWorktreeNames(t *testing.T) {
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

	parentDir := filepath.Dir(repoDir)
	featurePath := filepath.Join(parentDir, "feature-auth")
	cmd = exec.Command("git", "worktree", "add", "-b", "feature/auth", featurePath, "main")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	gitDir := filepath.Join(repoDir, ".git")

	// Test that completions include the worktree folder name
	completions, directive := completeWorktreeNamesFromGitDir(gitDir, nil, []string{}, "")

	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
	assert.Contains(t, completions, "feature-auth")
}

func TestCompleteWorktreeNames_NoCompletionsWhenArgAlreadyProvided(t *testing.T) {
	completions, directive := completeWorktreeNamesFromGitDir("", nil, []string{"existing-arg"}, "")

	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
	assert.Empty(t, completions)
}
