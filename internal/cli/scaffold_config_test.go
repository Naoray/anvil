package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
)

func TestScaffoldUsesSelectedWorktreeConfig(t *testing.T) {
	for _, mode := range []string{"current", "named", "work"} {
		t.Run(mode, func(t *testing.T) {
			repo, wt, env := promptPolicyFixture(t, true)
			require.NoError(t, os.WriteFile(filepath.Join(repo, "anvil.yaml"), []byte("scaffold:\n  override: true\n  steps:\n    - name: file.copy\n      from: README.md\n      to: source-proof\n"), 0o644))
			require.NoError(t, os.WriteFile(filepath.Join(wt, "anvil.yaml"), []byte("scaffold:\n  override: true\n  steps:\n    - name: file.copy\n      from: README.md\n      to: target-proof\n"), 0o644))
			cwd, args := wt, []string{"scaffold", "--no-interactive"}
			if mode == "named" {
				cwd, args = repo, []string{"scaffold", "feature-x", "--no-interactive"}
			}
			if mode == "work" {
				runGitCommand(t, wt, "add", "anvil.yaml")
				runGitCommand(t, wt, "commit", "-m", "Target scaffold fixture")
				cmd := exec.Command("git", "worktree", "remove", wt)
				cmd.Dir = repo
				output, err := cmd.CombinedOutput()
				require.NoError(t, err, string(output))
				cwd, args = repo, []string{"work", "feature-x", "--no-interactive"}
			}
			output, err := runPromptPolicyCommand(t, "", cwd, env, "", args...)
			require.NoError(t, err, output)
			require.FileExists(t, filepath.Join(wt, "target-proof"), output)
			require.NoFileExists(t, filepath.Join(wt, "source-proof"))
		})
	}
}

func TestWorktreeConfigKeepsLinkedDefaults(t *testing.T) {
	dir := t.TempDir()
	linked := &config.Config{SiteName: "linked-site", Preset: "php", DefaultBranch: "main", EditorCmd: "code", Cleanup: config.CleanupConfig{Steps: []config.CleanupStep{{Name: "db.destroy"}}}}
	pc := &ProjectContext{Config: linked}
	require.NoError(t, pc.LoadWorktreeConfig(dir))
	require.Same(t, linked, pc.Config)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "anvil.yaml"), []byte("scaffold:\n  override: true\n  steps: []\n"), 0o644))
	require.NoError(t, pc.LoadWorktreeConfig(dir))
	require.Equal(t, "linked-site", pc.Config.SiteName)
	require.Equal(t, "php", pc.Config.Preset)
	require.Equal(t, "main", pc.Config.DefaultBranch)
	require.Equal(t, "code", pc.Config.EditorCmd)
	require.Equal(t, linked.Cleanup, pc.Config.Cleanup)
	require.True(t, pc.Config.Scaffold.Override)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "anvil.yaml"), []byte("preset: laravel\nsite_name: target-site\ncleanup:\n  steps: []\n"), 0o644))
	require.NoError(t, pc.LoadWorktreeConfig(dir))
	require.Equal(t, "laravel", pc.Config.Preset)
	require.Equal(t, "target-site", pc.Config.SiteName)
	require.Empty(t, pc.Config.Cleanup.Steps)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "anvil.yaml"), []byte("scaffold: ["), 0o644))
	require.ErrorContains(t, pc.LoadWorktreeConfig(dir), "loading worktree configuration")
}

func TestTargetConfigRetainsDatabaseFlags(t *testing.T) {
	for _, mode := range []string{"scaffold", "work"} {
		for _, control := range []struct{ flag, value, want string }{
			{"--db-action", "NO_SUCH_DB=migrate", "database target"},
			{"--reset-database", "NO_SUCH_DB", "database target"},
			{"--borrow-app-db", "", "requires an application db.create"},
		} {
			t.Run(mode+"/"+control.flag, func(t *testing.T) {
				repo, wt, env := promptPolicyFixture(t, true)
				require.NoError(t, os.WriteFile(filepath.Join(wt, "anvil.yaml"), []byte("scaffold:\n  override: true\n  steps: []\n"), 0o644))
				if mode == "work" {
					runGitCommand(t, wt, "add", "anvil.yaml")
					runGitCommand(t, wt, "commit", "-m", "Target database flags fixture")
					runGitCommand(t, repo, "worktree", "remove", wt)
				}
				value := control.value
				if control.flag == "--borrow-app-db" {
					value = repo
				}
				output, err := runPromptPolicyCommand(t, "", repo, env, "", mode, "feature-x", "--no-interactive", control.flag, value)
				require.Error(t, err, output)
				require.Contains(t, output, control.want)
				require.NotContains(t, output, "Worktree ready")
				require.NotContains(t, output, "Scaffold complete")
				if control.flag == "--borrow-app-db" {
					require.NoFileExists(t, filepath.Join(wt, ".anvil.local"))
				}
			})
		}
	}
}

func TestFreshLaravelSQLiteDryRunPlansMigrationWithoutEffects(t *testing.T) {
	repo, wt, env := promptPolicyFixture(t, true)
	for name, content := range map[string]string{"composer.json": "{}\n", "artisan": "<?php\n", ".env.example": "DB_CONNECTION=sqlite\nDB_DATABASE=database/planned.sqlite\n", "anvil.yaml": "preset: laravel\n"} {
		require.NoError(t, os.WriteFile(filepath.Join(wt, name), []byte(content), 0o644))
	}
	output, err := runPromptPolicyCommand(t, "", repo, env, "", "scaffold", "feature-x", "--dry-run", "--force", "--no-interactive", "--verbose")
	require.NoError(t, err, output)
	require.Contains(t, output, "database/planned.sqlite role=application engine=sqlite service=sqlite action=migrate")
	require.Contains(t, output, "seed=true")
	require.NoFileExists(t, filepath.Join(wt, ".env"))
	require.NoFileExists(t, filepath.Join(wt, ".anvil.local"))
	require.NoFileExists(t, filepath.Join(wt, "database/planned.sqlite"))
}
