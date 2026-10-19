package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/git"
)

func TestWorkWarmFlags(t *testing.T) {
	bin := getAnvilBinary(t)
	for _, args := range [][]string{{"work", "--help"}, {"work", "new", "--copy-mode", "copy"}} {
		cmd := exec.Command(bin, args...)
		cmd.Env = subprocessEnv(t)
		out, err := cmd.CombinedOutput()
		if args[1] == "--help" {
			require.NoError(t, err)
			require.Contains(t, string(out), "--warm-from")
			require.Contains(t, string(out), "--copy-mode")
		} else {
			require.Error(t, err)
			require.Contains(t, string(out), "--copy-mode requires --warm-from")
		}
	}
}
func TestWarmSourceOnlyRegisteredSameRepository(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	gitDir, repo := createTestRepo(t)
	source := filepath.Join(t.TempDir(), "source")
	require.NoError(t, git.CreateWorktree(gitDir, source, "source", "main"))
	pc := &ProjectContext{GitDir: gitDir, ProjectPath: repo}
	resolved, err := resolveWarmSource(pc, "source")
	require.NoError(t, err)
	canonical, err := filepath.EvalSymlinks(source)
	require.NoError(t, err)
	require.Equal(t, canonical, resolved)
	_, err = resolveWarmSource(pc, "missing")
	require.Error(t, err)
	otherGit, otherRepo := createTestRepo(t)
	_ = otherGit
	// A corrupted registered worktree pointer must not make another repository a source.
	require.NoError(t, os.WriteFile(filepath.Join(source, ".git"), []byte("gitdir: "+filepath.Join(otherRepo, ".git")+"\n"), 0644))
	_, err = resolveWarmSource(pc, "source")
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "repository") || strings.Contains(err.Error(), "worktree"))
}
