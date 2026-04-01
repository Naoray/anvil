package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestRepoForLink(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmds := [][]string{
		{"git", "init", "-b", "main", dir},
		{"git", "-C", dir, "config", "user.email", "test@test.com"},
		{"git", "-C", dir, "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		require.NoError(t, cmd.Run(), "setup: %v", args)
	}
	// Create initial commit so HEAD exists
	readme := filepath.Join(dir, "README.md")
	require.NoError(t, os.WriteFile(readme, []byte("test"), 0644))
	cmd := exec.Command("git", "-C", dir, "add", ".")
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "-C", dir, "commit", "-m", "init")
	require.NoError(t, cmd.Run())
	return dir
}

func TestDeriveProjectName(t *testing.T) {
	t.Run("uses --name flag when provided", func(t *testing.T) {
		repoDir := createTestRepoForLink(t)
		name := deriveProjectName(repoDir, "custom-name")
		assert.Equal(t, "custom-name", name)
	})

	t.Run("derives name from origin remote URL", func(t *testing.T) {
		repoDir := createTestRepoForLink(t)

		// Add a remote
		cmd := exec.Command("git", "-C", repoDir, "remote", "add", "origin", "git@github.com:Naoray/scribe.git")
		require.NoError(t, cmd.Run())

		name := deriveProjectName(repoDir, "")
		assert.Equal(t, "scribe", name)
	})

	t.Run("derives name from HTTPS remote URL", func(t *testing.T) {
		repoDir := createTestRepoForLink(t)

		cmd := exec.Command("git", "-C", repoDir, "remote", "add", "origin", "https://github.com/naoray/anvil.git")
		require.NoError(t, cmd.Run())

		name := deriveProjectName(repoDir, "")
		assert.Equal(t, "anvil", name)
	})

	t.Run("falls back to directory name when no remote", func(t *testing.T) {
		repoDir := createTestRepoForLink(t)
		name := deriveProjectName(repoDir, "")
		assert.Equal(t, filepath.Base(repoDir), name)
	})

	t.Run("flag overrides remote name", func(t *testing.T) {
		repoDir := createTestRepoForLink(t)

		cmd := exec.Command("git", "-C", repoDir, "remote", "add", "origin", "git@github.com:Naoray/scribe.git")
		require.NoError(t, cmd.Run())

		name := deriveProjectName(repoDir, "my-override")
		assert.Equal(t, "my-override", name)
	})
}
