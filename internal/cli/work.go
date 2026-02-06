package cli

import (
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/ui"
)

var workCmd = &cobra.Command{
	Use:   "work [BRANCH] [PATH]",
	Short: "Create or checkout a feature worktree",
	Long: `Creates or checks out a new worktree for a feature branch.

Arguments:
  BRANCH  Name of the feature branch
  PATH    Optional custom path (defaults to sanitised branch name)

If no branch is provided, interactive mode allows selection from
available branches or entering a new branch name.`,
	Args: cobra.RangeArgs(0, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		pc, err := OpenProjectFromCWD()
		if err != nil {
			return err
		}

		baseBranch := mustGetString(cmd, "base")
		dryRun := mustGetBool(cmd, "dry-run")
		verbose := mustGetBool(cmd, "verbose")
		quiet := mustGetBool(cmd, "quiet")

		var branch string
		if len(args) > 0 {
			branch = args[0]
		} else if ui.IsInteractive() {
			localBranches, err := git.ListAllBranches(pc.GitDir)
			if err != nil {
				return fmt.Errorf("listing local branches: %w", err)
			}

			remoteBranches, _ := git.ListRemoteBranches(pc.GitDir)

			selected, err := ui.SelectBranchInteractive(pc.GitDir, localBranches, remoteBranches)
			if err != nil {
				return fmt.Errorf("selecting branch: %w", err)
			}
			branch = selected
		}

		if branch == "" {
			return fmt.Errorf("branch name required (run interactively or provide branch as argument)")
		}

		if baseBranch == "" {
			baseBranch = pc.DefaultBranch
		}

		worktreePath := ""
		if len(args) > 1 {
			worktreePath = args[1]
		} else {
			worktreePath = pc.GetWorktreePath(branch)
		}

		absWorktreePath, err := filepath.Abs(worktreePath)
		if err != nil {
			return fmt.Errorf("getting absolute path: %w", err)
		}

		// Check for existing worktrees
		worktrees, err := git.ListWorktrees(pc.GitDir)
		if err != nil {
			return fmt.Errorf("listing worktrees: %w", err)
		}

		for _, wt := range worktrees {
			if wt.Branch == branch {
				ui.PrintInfo(fmt.Sprintf("Worktree already exists at %s", wt.Path))
				return nil
			}
		}

		ui.PrintStep(fmt.Sprintf("Creating worktree for branch '%s' from '%s'", branch, baseBranch))
		ui.PrintInfo(fmt.Sprintf("Path: %s", absWorktreePath))

		if !dryRun {
			if err := git.CreateWorktree(pc.GitDir, absWorktreePath, branch, baseBranch); err != nil {
				return fmt.Errorf("creating worktree: %w", err)
			}
		} else {
			ui.PrintInfo("[DRY RUN] Would create worktree")
		}

		// Set up branch tracking unless --no-track is specified
		noTrack := mustGetBool(cmd, "no-track")
		if !dryRun && !noTrack {
			if err := git.SetBranchUpstream(pc.GitDir, branch, "origin"); err != nil {
				// Non-fatal - just inform user if verbose
				if verbose {
					ui.PrintInfo(fmt.Sprintf("Could not set up tracking for branch '%s': %v", branch, err))
				}
			} else {
				ui.PrintSuccess(fmt.Sprintf("Set up tracking for branch '%s' on origin", branch))
			}
		}

		if !dryRun {
			preset := pc.Config.Preset
			if preset == "" {
				preset = pc.PresetManager().Detect(absWorktreePath)
			}

			if verbose && preset != "" {
				ui.PrintInfo(fmt.Sprintf("Running scaffold for preset: %s", preset))
			}

			repoName := filepath.Base(filepath.Dir(absWorktreePath))
			folderName := filepath.Base(absWorktreePath)

			// For the default branch, use the saved SiteName from project config
			// For feature branches, use the worktree folder name
			siteName := folderName
			if branch == pc.DefaultBranch && pc.Config.SiteName != "" {
				siteName = pc.Config.SiteName
			}

			if err := pc.ScaffoldManager().RunScaffold(absWorktreePath, branch, repoName, siteName, preset, pc.Config, false, verbose, quiet); err != nil {
				ui.PrintErrorWithHint("Scaffold steps failed", err.Error())
			}

			// Check if .anvil.local should be gitignored
			if !quiet {
				checkAnvilLocalGitignore(absWorktreePath)
			}
		} else {
			ui.PrintInfo("[DRY RUN] Would run scaffold steps")
		}

		ui.PrintDone(fmt.Sprintf("Worktree ready at %s", absWorktreePath))
		return nil
	},
}

func isCommandAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func init() {
	rootCmd.AddCommand(workCmd)

	workCmd.Flags().StringP("base", "b", "", "Base branch for new worktree")
	workCmd.Flags().Bool("no-track", false, "Skip setting up remote tracking for new branches")
}
