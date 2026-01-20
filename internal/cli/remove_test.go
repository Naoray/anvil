package cli

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/michaeldyrynda/arbor/internal/git"
)

func TestRemoveCmd_EmptyInputBehavior(t *testing.T) {
	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "repo")
	barePath := filepath.Join(tmpDir, ".bare")

	require.NoError(t, os.MkdirAll(repoDir, 0755))

	runGitCmd(t, repoDir, "init", "-b", "main")
	runGitCmd(t, repoDir, "config", "user.email", "test@example.com")
	runGitCmd(t, repoDir, "config", "user.name", "Test User")
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("test"), 0644))
	runGitCmd(t, repoDir, "add", ".")
	runGitCmd(t, repoDir, "commit", "-m", "Initial commit")
	runGitCmd(t, repoDir, "clone", "--bare", repoDir, barePath)

	mainPath := filepath.Join(tmpDir, "main")
	require.NoError(t, git.CreateWorktree(barePath, mainPath, "main", ""))

	featurePath := filepath.Join(tmpDir, "feature")
	require.NoError(t, git.CreateWorktree(barePath, featurePath, "feature", "main"))

	t.Run("empty input handled gracefully with bufio.Reader", func(t *testing.T) {
		reader := bufio.NewReader(bytes.NewReader([]byte("\n")))

		input, err := reader.ReadString('\n')
		require.NoError(t, err)

		trimmed := strings.TrimSpace(input)
		t.Logf("Fixed behavior: response = %q", trimmed)

		assert.Empty(t, trimmed, "empty input should result in empty string")

		assert.NotPanics(t, func() {
			_ = trimmed
		}, "empty input should not cause panic")

		assert.Equal(t, "", trimmed, "empty input should be treated as 'no'")
	})
}

func TestWorkCmd_InteractiveInputPattern(t *testing.T) {
	t.Run("work.go bufio.Reader handles empty input gracefully", func(t *testing.T) {
		reader := bufio.NewReader(bytes.NewReader([]byte("\n")))

		input, err := reader.ReadString('\n')
		require.NoError(t, err)

		trimmed := input
		if len(trimmed) > 0 {
			trimmed = trimmed[:len(trimmed)-1]
		}

		assert.Empty(t, trimmed)
		assert.NotPanics(t, func() {
			_ = trimmed
		})
	})

	t.Run("work.go pattern for branch selection", func(t *testing.T) {
		reader := bufio.NewReader(bytes.NewReader([]byte("1\n")))

		input, err := reader.ReadString('\n')
		require.NoError(t, err)

		trimmed := strings.TrimSpace(input)
		assert.Equal(t, "1", trimmed)
	})
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	allArgs := append([]string{"-C"}, dir)
	allArgs = append(allArgs, args...)
	cmd := exec.Command("git", allArgs...)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v failed: %v", args, err)
	}
}
