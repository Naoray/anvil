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

func TestTargetDefaultBranchSiteIdentity(t *testing.T) {
	for _, mode := range []string{"scaffold", "work"} {
		t.Run(mode, func(t *testing.T) {
			repo, wt, env := promptPolicyFixture(t, true)
			require.NoError(t, os.WriteFile(filepath.Join(wt, "anvil.yaml"), []byte(`default_branch: feature-x
site_name: target-site
scaffold:
  override: true
  steps:
    - name: env.write
      key: SITE_PROOF
      value: "{{ .SiteName }}"
`), 0o644))
			args := []string{"scaffold", "feature-x", "--no-interactive"}
			if mode == "work" {
				runBranchSelectionGit(t, wt, "add", "anvil.yaml")
				runGitCommand(t, wt, "commit", "-m", "Target site identity fixture")
				runBranchSelectionGit(t, repo, "worktree", "remove", wt)
				args = []string{"work", "feature-x", "--no-interactive"}
			}
			output, err := runPromptPolicyCommand(t, "", repo, env, "", args...)
			require.NoError(t, err, output)
			content, err := os.ReadFile(filepath.Join(wt, ".env"))
			require.NoError(t, err)
			require.Contains(t, string(content), "SITE_PROOF=target-site")
		})
	}
}

func TestRemoveUsesTargetCleanupConfig(t *testing.T) {
	for _, preset := range []string{"php", ""} {
		name := preset
		if name == "" {
			name = "no-preset"
		}
		t.Run(name, func(t *testing.T) {
			repo, wt, env := promptPolicyFixture(t, true)
			marker := filepath.Join(t.TempDir(), "cleanup-marker")
			command := fmt.Sprintf("echo target >> %q", filepath.ToSlash(marker))
			yaml := fmt.Sprintf("preset: %q\ncleanup:\n  steps:\n    - name: command.run\n      condition:\n        command: %q\n", preset, command)
			require.NoError(t, os.WriteFile(filepath.Join(wt, "anvil.yaml"), []byte(yaml), 0o644))
			output, err := runPromptPolicyCommand(t, "", repo, env, "", "remove", "feature-x", "--force", "--keep-db", "--no-interactive", "--verbose")
			require.NoError(t, err, output)
			content, err := os.ReadFile(marker)
			require.NoError(t, err, output)
			require.Equal(t, "target", strings.TrimSpace(string(content)))
			require.NoDirExists(t, wt)
			if preset != "" {
				require.Contains(t, output, "Running cleanup for preset: php")
			}
		})
	}
}

func TestRemoveRejectsMalformedTargetCleanupBeforeEffects(t *testing.T) {
	repo, wt, env := promptPolicyFixture(t, true)
	marker := filepath.Join(t.TempDir(), "cleanup-marker")
	command := fmt.Sprintf("echo target > %q", filepath.ToSlash(marker))
	yaml := fmt.Sprintf("preset: php\ncleanup:\n  steps:\n    - name: command.run\n      condition:\n        command: %q\n    - name: command.run\n      condition:\n        command: echo unused\n        context_vra: {name: Decision, value: 'true'}\n", command)
	require.NoError(t, os.WriteFile(filepath.Join(wt, "anvil.yaml"), []byte(yaml), 0o644))
	output, err := runPromptPolicyCommand(t, "", repo, env, "", "remove", "feature-x", "--force", "--keep-db", "--no-interactive")
	require.Error(t, err, output)
	require.Contains(t, output, "condition")
	require.DirExists(t, wt)
	require.NoFileExists(t, marker)
	require.NoFileExists(t, filepath.Join(wt, ".anvil.local"))
	actual, err := os.ReadFile(filepath.Join(wt, "anvil.yaml"))
	require.NoError(t, err)
	require.Equal(t, yaml, string(actual))
}

func TestPruneDoesNotReusePreviousTargetConfig(t *testing.T) {
	repo, first, env := promptPolicyFixture(t, true)
	second := filepath.Join(filepath.Dir(first), "second")
	runBranchSelectionGit(t, repo, "worktree", "add", "-b", "second", second)
	remote := filepath.Join(t.TempDir(), "remote.git")
	cmd := exec.Command("git", "clone", "--bare", repo, remote)
	outputBytes, err := cmd.CombinedOutput()
	require.NoError(t, err, string(outputBytes))
	runBranchSelectionGit(t, repo, "remote", "add", "origin", remote)
	marker := filepath.Join(t.TempDir(), "cleanup-marker")
	command := fmt.Sprintf("echo first >> %q", filepath.ToSlash(marker))
	yaml := fmt.Sprintf("preset: php\ndefault_branch: feature-x\nsite_name: target-site\ncleanup:\n  steps:\n    - name: command.run\n      condition:\n        command: %q\n", command)
	require.NoError(t, os.WriteFile(filepath.Join(first, "anvil.yaml"), []byte(yaml), 0o644))
	output, err := runPromptPolicyCommand(t, "", repo, env, "", "prune", "--force", "--keep-db", "--no-interactive", "--verbose")
	require.NoError(t, err, output)
	content, err := os.ReadFile(marker)
	require.NoError(t, err, output)
	require.Equal(t, "first", strings.TrimSpace(string(content)), "first target cleanup must run exactly once")
	require.Equal(t, 1, strings.Count(output, "Running cleanup for preset: php"), output)
	require.NoDirExists(t, first)
	require.NoDirExists(t, second)
}
