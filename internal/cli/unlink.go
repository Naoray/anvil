package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/ui"
)

var unlinkCmd = &cobra.Command{
	Use:   "unlink [NAME]",
	Short: "Unlink a project from centralized worktree management",
	Long: `Unlinks a project from anvil's centralized worktree management.

This removes the project registration from the global config. By default,
existing worktrees are preserved. Use --clean to remove them.

Arguments:
  NAME  Name of the linked project (defaults to current directory's project)

Examples:
  # Unlink current project
  anvil unlink

  # Unlink by name
  anvil unlink my-project

  # Unlink and remove all worktrees
  anvil unlink --clean

  # Preview the cleanup (never prompts, changes nothing)
  anvil unlink --clean --dry-run`,
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

		dryRun := mustGetBool(cmd, "dry-run")
		verbose := mustGetBool(cmd, "verbose")
		quiet := mustGetBool(cmd, "quiet")
		clean := mustGetBool(cmd, "clean")
		force := mustGetBool(cmd, "force")

		// Get worktree base (best-effort; empty worktreeBase means no centralized worktrees)
		worktreeBase, _ := globalCfg.GetWorktreeBaseExpanded()
		projectWorktreeDir := ""
		if worktreeBase != "" {
			projectWorktreeDir = filepath.Join(worktreeBase, projectName)
		}

		// Check for existing worktrees
		if projectWorktreeDir != "" {
			if info, err := os.Stat(projectWorktreeDir); err == nil && info.IsDir() {
				entries, err := os.ReadDir(projectWorktreeDir)
				if err != nil {
					return fmt.Errorf("reading worktree directory: %w", err)
				}
				if len(entries) > 0 {
					if clean {
						if !force && !dryRun {
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

						pc, err := openProject(projectInfo.Path, projectName, projectInfo, globalCfg)
						if err != nil {
							return err
						}
						worktrees, err := git.ListWorktreesDetailed(pc.GitDir, pc.CWD, pc.DefaultBranch)
						if err != nil {
							return err
						}
						var selected []git.Worktree
						for _, wt := range worktrees {
							if filepath.Dir(wt.Path) == projectWorktreeDir {
								selected = append(selected, wt)
							}
						}
						if err := orderDatabaseRemovals(selected); err != nil {
							return err
						}
						deps := defaultRemoveCommandDependencies().lifecycleDependencies()
						for _, wt := range selected {
							cleanupOpts, messages, err := planWorktreeCleanup(wt.Path, false, dryRun, force, config.ReadLocalState)
							if err != nil {
								return err
							}
							if err := runRemoveLifecycle(pc, wt, removeLifecycleOptions{DryRun: dryRun, Verbose: verbose, Quiet: quiet, IgnoreLease: mustGetBool(cmd, "ignore-lease")}, cleanupOpts, messages, deps); err != nil {
								return err
							}
						}
						if dryRun {
							ui.PrintInfo("[DRY RUN] Would unlink project after worktree cleanup")
							return nil
						}
						if err := os.Remove(projectWorktreeDir); err != nil {
							return fmt.Errorf("removing worktree directory (unregistered files are preserved): %w", err)
						}
						ui.PrintSuccess(fmt.Sprintf("Removed worktrees in %s", projectWorktreeDir))
					} else {
						ui.PrintWarning(fmt.Sprintf("Worktrees exist at %s (use --clean to remove)", projectWorktreeDir))
					}
				}
			}
		}

		if dryRun {
			ui.PrintInfo("[DRY RUN] Would unlink project registration")
			return nil
		}
		// Remove from global config
		globalCfg.RemoveProject(projectName)

		// Save global config
		if err := config.SaveGlobalConfig(globalCfg); err != nil {
			return fmt.Errorf("saving global config: %w", err)
		}

		ui.PrintSuccess(fmt.Sprintf("Unlinked '%s'", projectName))
		ui.PrintInfo(fmt.Sprintf("Project at %s is no longer managed by anvil", projectInfo.Path))

		return nil
	},
}

func init() {
	rootCmd.AddCommand(unlinkCmd)

	unlinkCmd.Flags().Bool("clean", false, "Remove all worktrees for this project")
	unlinkCmd.Flags().Bool("force", false, "Skip confirmation when using --clean")
	unlinkCmd.Flags().Bool("ignore-lease", false, "Remove even if another agent holds an active lease (explicit authorization)")
}
