package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/artisanexperiences/arbor/internal/config"
	"github.com/artisanexperiences/arbor/internal/git"
	"github.com/artisanexperiences/arbor/internal/ui"
)

var unlinkCmd = &cobra.Command{
	Use:   "unlink [NAME]",
	Short: "Unlink a project from centralized worktree management",
	Long: `Unlinks a project from arbor's centralized worktree management.

This removes the project registration from the global config. By default,
existing worktrees are preserved. Use --clean to remove them.

Arguments:
  NAME  Name of the linked project (defaults to current directory's project)

Examples:
  # Unlink current project
  arbor unlink

  # Unlink by name
  arbor unlink my-project

  # Unlink and remove all worktrees
  arbor unlink --clean`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Load global config
		globalCfg, err := config.LoadOrCreateGlobalConfig()
		if err != nil {
			return fmt.Errorf("loading global config: %w", err)
		}

		// Determine which project to unlink
		var projectName string
		var projectInfo *config.ProjectInfo

		if len(args) > 0 {
			projectName = args[0]
			projectInfo = globalCfg.GetLinkedProjectByName(projectName)
		} else {
			// Try to find from current directory
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting current directory: %w", err)
			}

			projectName, projectInfo = globalCfg.FindLinkedProjectFromPath(cwd)
		}

		if projectInfo == nil {
			if projectName != "" {
				return fmt.Errorf("project '%s' is not linked", projectName)
			}
			return fmt.Errorf("current directory is not inside a linked project. Specify the project name as an argument")
		}

		clean := mustGetBool(cmd, "clean")
		force := mustGetBool(cmd, "force")

		// Get worktree base
		worktreeBase, _ := globalCfg.GetWorktreeBaseExpanded()
		projectWorktreeDir := ""
		if worktreeBase != "" {
			projectWorktreeDir = filepath.Join(worktreeBase, projectName)
		}

		// Check for existing worktrees
		if projectWorktreeDir != "" {
			if info, err := os.Stat(projectWorktreeDir); err == nil && info.IsDir() {
				entries, _ := os.ReadDir(projectWorktreeDir)
				if len(entries) > 0 {
					if clean {
						if !force {
							confirmed, err := ui.Confirm(
								fmt.Sprintf("Remove %d worktree(s) in %s?", len(entries), projectWorktreeDir),
							)
							if err != nil {
								return fmt.Errorf("confirmation: %w", err)
							}
							if !confirmed {
								ui.PrintInfo("Cancelled")
								return nil
							}
						}

						// Remove worktrees from git first
						if _, _, err := git.FindGitDir(projectInfo.Path); err == nil {
							for _, entry := range entries {
								if entry.IsDir() {
									worktreePath := filepath.Join(projectWorktreeDir, entry.Name())
									_ = git.RemoveWorktree(worktreePath, true)
								}
							}
						}

						// Remove the directory
						if err := os.RemoveAll(projectWorktreeDir); err != nil {
							ui.PrintWarning(fmt.Sprintf("Failed to remove worktree directory: %s", err))
						} else {
							ui.PrintSuccess(fmt.Sprintf("Removed worktrees in %s", projectWorktreeDir))
						}
					} else {
						ui.PrintWarning(fmt.Sprintf("Worktrees exist at %s (use --clean to remove)", projectWorktreeDir))
					}
				}
			}
		}

		// Remove from global config
		globalCfg.RemoveProject(projectName)

		// Save global config
		if err := config.SaveGlobalConfig(globalCfg); err != nil {
			return fmt.Errorf("saving global config: %w", err)
		}

		ui.PrintSuccess(fmt.Sprintf("Unlinked '%s'", projectName))
		ui.PrintInfo(fmt.Sprintf("Project at %s is no longer managed by arbor", projectInfo.Path))

		return nil
	},
}

func init() {
	rootCmd.AddCommand(unlinkCmd)

	unlinkCmd.Flags().Bool("clean", false, "Remove all worktrees for this project")
	unlinkCmd.Flags().Bool("force", false, "Skip confirmation when using --clean")
}
