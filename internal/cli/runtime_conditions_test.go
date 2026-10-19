package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRemoveCommand_InvalidRuntimeGuardFailsBeforeCleanupEffects(t *testing.T) {
	_, repo := createTestRepo(t)
	worktree := filepath.Join(t.TempDir(), "runtime")
	cmd := exec.Command("git", "-C", repo, "worktree", "add", "-b", "runtime", worktree)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	env := subprocessEnv(t, "CI=true")
	configDir := ""
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, "XDG_CONFIG_HOME="); ok {
			configDir = filepath.Join(value, "anvil")
		}
	}
	require.NotEmpty(t, configDir)
	require.NoError(t, os.MkdirAll(configDir, 0700))
	for _, guard := range []string{
		"context_vra: {name: Decision, value: 'true'}",
		"context_var: {name: Decision, value: true}",
		"not: [false]",
	} {
		content := fmt.Sprintf(`setup_complete: true
worktree_base: %q
projects:
  runtime-conditions:
    path: %q
    default_branch: main
    preset: php
    cleanup:
      steps:
        - name: command.run
          condition:
            command: echo side-effect > effect
        - name: command.run
          condition:
            command: echo unused
            %s
`, filepath.ToSlash(filepath.Dir(worktree)), filepath.ToSlash(repo), guard)
		configPath := filepath.Join(configDir, "anvil.yaml")
		require.NoError(t, os.WriteFile(configPath, []byte(content), 0600))
		cmd = exec.Command(getAnvilBinary(t), "remove", "runtime", "--force", "--keep-db", "--no-interactive")
		cmd.Dir = repo
		cmd.Env = env
		output, err = cmd.CombinedOutput()
		require.Error(t, err, "guard %s: %s", guard, output)
		require.Contains(t, string(output), "condition")
		require.DirExists(t, worktree)
		require.NoFileExists(t, filepath.Join(worktree, "effect"))
		require.NoFileExists(t, filepath.Join(worktree, ".anvil.local"))
		actual, readErr := os.ReadFile(configPath)
		require.NoError(t, readErr)
		require.Equal(t, content, string(actual))
	}
}
