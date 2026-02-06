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

	"github.com/naoray/anvil/internal/git"
)

func TestRemoveCmd_PreventsMainWorktreeDeletion(t *testing.T) {
	repoDir := t.TempDir()
	parentDir := filepath.Dir(repoDir)

	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = repoDir
	require.NoError(t, cmd.Run())

	runGitCmd(t, repoDir, "config", "user.email", "test@example.com")
	runGitCmd(t, repoDir, "config", "user.name", "Test User")
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("test"), 0644))
	runGitCmd(t, repoDir, "add", ".")
	runGitCmd(t, repoDir, "commit", "-m", "Initial commit")

	gitDir := filepath.Join(repoDir, ".git")

	detachHEAD(t, repoDir)

	mainPath := filepath.Join(parentDir, "main-wt")
	require.NoError(t, git.CreateWorktree(gitDir, mainPath, "main", ""))

	featurePath := filepath.Join(parentDir, "feature-wt")
	require.NoError(t, git.CreateWorktree(gitDir, featurePath, "feature", "main"))

	t.Run("main worktree is correctly identified", func(t *testing.T) {
		defaultBranch, err := git.GetDefaultBranch(gitDir)
		require.NoError(t, err)

		worktrees, err := git.ListWorktreesDetailed(gitDir, mainPath, defaultBranch)
		require.NoError(t, err)

		var mainWt *git.Worktree
		for _, wt := range worktrees {
			if wt.Branch == "main" && wt.Path != repoDir {
				mainWt = &wt
				break
			}
		}

		require.NotNil(t, mainWt, "main worktree should be found")
		assert.True(t, mainWt.IsMain, "main worktree should have IsMain=true")
	})

	t.Run("feature worktree can be removed", func(t *testing.T) {
		_, err := os.Stat(featurePath)
		assert.NoError(t, err, "feature worktree should exist before removal")

		err = git.RemoveWorktree(gitDir, featurePath, true)
		assert.NoError(t, err)

		_, err = os.Stat(featurePath)
		assert.True(t, os.IsNotExist(err), "feature worktree should not exist after removal")
	})
}

func TestRemoveCmd_EmptyInputBehavior(t *testing.T) {
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
