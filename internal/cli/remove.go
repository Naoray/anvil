package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	arborerrors "github.com/michaeldyrynda/arbor/internal/errors"
	"github.com/michaeldyrynda/arbor/internal/git"
	"github.com/michaeldyrynda/arbor/internal/ui"
)

var removeCmd = &cobra.Command{
	Use:   "remove [FOLDER]",
	Short: "Remove a worktree with cleanup",
	Long: `Removes a worktree and runs preset-defined cleanup steps.

Arguments:
  FOLDER  Name of the worktree folder to remove (e.g., feature-test-change)

Cleanup steps may include:
  - Removing Herd site links
  - Database cleanup prompts`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		pc, err := OpenProjectFromCWD()
		if err != nil {
			return err
		}

		force := mustGetBool(cmd, "force")
		dryRun := mustGetBool(cmd, "dry-run")
		verbose := mustGetBool(cmd, "verbose")

		currentWorktreePath, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting current directory: %w", err)
		}

		defaultBranch, err := git.GetDefaultBranch(pc.BarePath)
		if err != nil {
			return fmt.Errorf("getting default branch: %w", err)
		}

		worktrees, err := git.ListWorktreesDetailed(pc.BarePath, currentWorktreePath, defaultBranch)
		if err != nil {
			return fmt.Errorf("listing worktrees: %w", err)
		}

		var targetWorktree *git.Worktree

		if len(args) > 0 {
			folderName := args[0]
			for _, wt := range worktrees {
				if filepath.Base(wt.Path) == folderName {
					targetWorktree = &wt
					break
				}
			}
			if targetWorktree == nil {
				return fmt.Errorf("worktree '%s' not found: %w", folderName, arborerrors.ErrWorktreeNotFound)
			}
		} else if ui.IsInteractive() {
			selected, err := ui.SelectWorktreeToRemove(worktrees)
			if err != nil {
				return fmt.Errorf("selecting worktree: %w", err)
			}
			targetWorktree = selected
		} else {
			return fmt.Errorf("worktree folder name required (run interactively or use --force to skip prompts)")
		}

		if targetWorktree.IsMain {
			return fmt.Errorf("cannot remove main worktree")
		}

		ui.PrintInfo(fmt.Sprintf("Removing %s at %s", targetWorktree.Branch, targetWorktree.Path))

		deleteBranch := false
		if !force {
			if !ui.IsInteractive() {
				return fmt.Errorf("worktree removal requires confirmation (use --force to skip)")
			}

			ui.PrintInfo("This will run cleanup steps.")
			confirmed, err := ui.Confirm(fmt.Sprintf("Remove worktree '%s'?", targetWorktree.Branch))
			if err != nil {
				return fmt.Errorf("confirmation: %w", err)
			}
			if !confirmed {
				ui.PrintInfo("Cancelled.")
				return nil
			}

			if git.BranchExists(pc.BarePath, targetWorktree.Branch) {
				deleteBranch, err = ui.Confirm(fmt.Sprintf("Also delete branch '%s'?", targetWorktree.Branch))
				if err != nil {
					return fmt.Errorf("branch deletion confirmation: %w", err)
				}
			}
		} else {
			deleteBranch = mustGetBool(cmd, "delete-branch")
		}

		ui.PrintStep("Removing worktree")

		if !dryRun {
			preset := pc.Config.Preset
			if preset == "" {
				preset = pc.PresetManager().Detect(targetWorktree.Path)
			}

			if verbose && preset != "" {
				ui.PrintInfo(fmt.Sprintf("Running cleanup for preset: %s", preset))
			}

			if preset != "" {
				siteName := filepath.Base(targetWorktree.Path)
				if err := pc.ScaffoldManager().RunCleanup(targetWorktree.Path, targetWorktree.Branch, "", siteName, preset, pc.Config, false, verbose); err != nil {
					ui.PrintErrorWithHint("Cleanup failed", err.Error())
				}
			}

			if err := git.RemoveWorktree(targetWorktree.Path, true); err != nil {
				return fmt.Errorf("removing worktree: %w", err)
			}
			ui.PrintSuccessPath("Removed", targetWorktree.Path)

			if deleteBranch && git.BranchExists(pc.BarePath, targetWorktree.Branch) {
				if err := git.DeleteBranch(pc.BarePath, targetWorktree.Branch, true); err != nil {
					ui.PrintErrorWithHint("Failed to delete branch", err.Error())
				} else {
					ui.PrintSuccess(fmt.Sprintf("Deleted branch '%s'", targetWorktree.Branch))
				}
			}

			parentDir := filepath.Dir(targetWorktree.Path)
			entries, err := os.ReadDir(parentDir)
			if err == nil && len(entries) == 0 {
				if err := os.Remove(parentDir); err != nil {
					ui.PrintErrorWithHint(fmt.Sprintf("Could not remove empty directory %s", parentDir), err.Error())
				}
			}
		} else {
			ui.PrintInfo("[DRY RUN] Would run cleanup and remove worktree")
			if deleteBranch {
				ui.PrintInfo("[DRY RUN] Would delete branch")
			}
		}

		ui.PrintDone("Worktree removed")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(removeCmd)

	removeCmd.Flags().BoolP("force", "f", false, "Skip confirmation and cleanup prompts")
	removeCmd.Flags().Bool("delete-branch", false, "Also delete the branch after removing worktree")
}
