package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScaffoldCommand_RuntimeConditions(t *testing.T) {
	_, dir := createTestRepo(t)
	binary := getAnvilBinary(t)
	env := subprocessEnv(t, "CI=true")
	run := func(args ...string) ([]byte, error) {
		cmd := exec.Command(binary, args...)
		cmd.Dir = dir
		cmd.Env = env
		return cmd.CombinedOutput()
	}
	output, err := run("link", "--name", "runtime-conditions", "--preset", "php", "--no-interactive")
	require.NoError(t, err, "%s", output)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "source"), []byte("sentinel"), 0600))
	configPath := filepath.Join(dir, "anvil.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(`scaffold:
  override: true
  steps:
    - name: command.run
      command: echo true
      store_as: Decision
    - name: file.copy
      from: source
      to: matched
      condition:
        context_var: {name: Decision, value: "true"}
    - name: file.copy
      from: source
      to: skipped
      condition:
        context_var: {name: Decision, value: "false"}
`), 0600))
	output, err = run("scaffold", "main", "--quiet", "--no-interactive")
	require.NoError(t, err, "%s", output)
	require.FileExists(t, filepath.Join(dir, "matched"))
	require.NoFileExists(t, filepath.Join(dir, "skipped"))
	statePath := filepath.Join(dir, ".anvil.local")
	state, err := os.ReadFile(statePath)
	require.NoError(t, err)
	for _, guard := range []string{
		"context_vra: {name: Decision, value: 'true'}",
		"context_var: {name: Decision, value: true}",
		"not: [false]",
	} {
		invalid := []byte("scaffold:\n  override: true\n  steps:\n    - name: file.copy\n      from: source\n      to: effect\n    - name: command.run\n      command: echo unused\n      enabled: false\n      condition:\n        " + guard + "\n")
		require.NoError(t, os.WriteFile(configPath, invalid, 0600))
		output, err = run("scaffold", "main", "--quiet", "--no-interactive")
		require.Error(t, err, "guard %s: %s", guard, output)
		require.NoFileExists(t, filepath.Join(dir, "effect"))
		actual, readErr := os.ReadFile(statePath)
		require.NoError(t, readErr)
		require.Equal(t, state, actual)
		actual, readErr = os.ReadFile(configPath)
		require.NoError(t, readErr)
		require.Equal(t, invalid, actual)
	}

	// A rejected first run must not create local state either.
	require.NoError(t, os.Remove(statePath))
	output, err = run("scaffold", "main", "--quiet", "--no-interactive")
	require.Error(t, err, "%s", output)
	require.NoFileExists(t, statePath)
	require.NoFileExists(t, filepath.Join(dir, "effect"))
}

func TestRemoteScaffoldRuntimeConditions(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		name := "pipe"
		if terminal {
			name = "pty"
		}
		t.Run(name, func(t *testing.T) {
			python := ""
			if terminal {
				if runtime.GOOS == "windows" {
					t.Skip("POSIX controlling terminal required")
				}
				var err error
				python, err = exec.LookPath("python3")
				if err != nil {
					t.Skip("python3 required for PTY integration")
				}
			}
			repo, wt, env := promptPolicyFixture(t, false)
			mainSHA := runBranchSelectionGitOutput(t, repo, "rev-parse", "HEAD")
			require.NoError(t, os.WriteFile(filepath.Join(wt, "anvil.yaml"), []byte(`scaffold:
  override: true
  steps:
    - name: command.run
      command: git rev-parse --is-inside-work-tree
      store_as: IsWorktree
    - name: file.copy
      from: README.md
      to: matched
      condition:
        context_var: {name: IsWorktree, value: "true"}
    - name: file.copy
      from: README.md
      to: skipped
      condition:
        context_var: {name: IsWorktree, value: "false"}
`), 0o644))
			runBranchSelectionGit(t, wt, "add", "anvil.yaml")
			runGitCommand(t, wt, "commit", "-m", "Remote scaffold fixture")
			featureSHA := runBranchSelectionGitOutput(t, wt, "rev-parse", "HEAD")
			require.NotEqual(t, mainSHA, featureSHA)
			runBranchSelectionGit(t, repo, "remote", "add", "company", "https://example.test/company.git")
			runBranchSelectionGit(t, repo, "update-ref", "refs/remotes/company/feature/runtime", featureSHA)
			runBranchSelectionGit(t, repo, "worktree", "remove", wt)
			runBranchSelectionGit(t, repo, "branch", "-D", "feature-x")
			// The invoking checkout's invalid YAML must not replace target configuration.
			require.NoError(t, os.WriteFile(filepath.Join(repo, "anvil.yaml"), []byte("scaffold: ["), 0o644))
			output, err := runPromptPolicyCommand(t, python, repo, env, "", "work", "company/feature/runtime", wt, "--no-interactive")
			require.NoError(t, err, output)
			require.NotContains(t, output, "PTY TIMEOUT")
			require.NotContains(t, output, "Welcome to Anvil!")
			require.Equal(t, featureSHA, runBranchSelectionGitOutput(t, wt, "rev-parse", "HEAD"))
			require.Equal(t, "refs/heads/feature/runtime", runBranchSelectionGitOutput(t, wt, "symbolic-ref", "HEAD"))
			require.Equal(t, "company/feature/runtime", runBranchSelectionGitOutput(t, wt, "rev-parse", "--abbrev-ref", "@{upstream}"))
			require.FileExists(t, filepath.Join(wt, "matched"))
			require.NoFileExists(t, filepath.Join(wt, "skipped"))
			require.NoFileExists(t, filepath.Join(repo, "matched"))
		})
	}
}
