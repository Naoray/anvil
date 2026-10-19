package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/scaffold"
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
		warmFrom := mustGetString(cmd, "warm-from")
		copyMode := mustGetString(cmd, "copy-mode")
		if warmFrom == "" && cmd.Flags().Changed("copy-mode") {
			return fmt.Errorf("--copy-mode requires --warm-from")
		}
		if copyMode != "auto" && copyMode != "copy" {
			return fmt.Errorf("--copy-mode must be auto or copy")
		}
		if warmFrom != "" && mustGetBool(cmd, "skip-scaffold") {
			return fmt.Errorf("--warm-from requires scaffold; remove --skip-scaffold")
		}

		pc, err := OpenProjectFromCWD()
		if err != nil {
			return err
		}

		warmSource := ""
		if warmFrom != "" {
			warmSource, err = resolveWarmSource(pc, warmFrom)
			if err != nil {
				return err
			}
		}
		baseBranch := mustGetString(cmd, "base")

		dryRun := mustGetBool(cmd, "dry-run")
		verbose := mustGetBool(cmd, "verbose")
		quiet := mustGetBool(cmd, "quiet")

		var branch string
		if len(args) > 0 {
			branch = args[0]
		} else if ui.ShouldPrompt(cmd, false) {
			localBranches, remoteBranches, err := branchRefsForSelection(pc.GitDir)
			if err != nil {
				return err
			}

			selected, err := ui.SelectBranchInteractive(pc.GitDir, localBranches, remoteBranches)
			if err != nil {
				return fmt.Errorf("selecting branch: %w", err)
			}
			branch = selected
		}

		if branch == "" {
			return fmt.Errorf("branch name required (run interactively or provide branch as argument)")
		}

		selection, err := git.ResolveWorkBranch(pc.GitDir, branch)
		if err != nil {
			return err
		}
		branch = selection.Branch
		if baseBranch == "" {
			baseBranch = selection.Base
			if baseBranch == "" {
				baseBranch = pc.DefaultBranch
			}
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
		if !dryRun && !noTrack && !selection.Existing {
			remote := selection.Remote
			if remote == "" {
				remote = config.DefaultRemote
			}
			if err := git.SetBranchUpstream(pc.GitDir, branch, remote); err != nil {
				// Non-fatal - just inform user if verbose
				if verbose {
					ui.PrintInfo(fmt.Sprintf("Could not set up tracking for branch '%s': %v", branch, err))
				}
			} else {
				ui.PrintSuccess(fmt.Sprintf("Set up tracking for branch '%s' on %s", branch, remote))
			}
		}

		skipScaffold := mustGetBool(cmd, "skip-scaffold")
		if skipScaffold {
			if !quiet {
				ui.PrintInfo("Scaffold skipped (run 'anvil scaffold' to set up later)")
			}
		} else if !dryRun {
			if err := pc.LoadWorktreeConfig(absWorktreePath); err != nil {
				return err
			}
			if err := applyDatabaseFlags(cmd, pc); err != nil {
				return err
			}

			resolvedPreset := pc.ResolvePreset("", absWorktreePath)

			if verbose && resolvedPreset.Name() != "" {
				ui.PrintInfo(fmt.Sprintf("Running scaffold for preset: %s", resolvedPreset.Name()))
			}

			repoName := filepath.Base(filepath.Dir(absWorktreePath))
			siteName := worktreeSiteName(absWorktreePath, branch, pc.DefaultBranch, pc.Config.SiteName)

			if err := pc.ScaffoldManager().RunScaffold(
				absWorktreePath,
				branch,
				repoName,
				siteName,
				resolvedPreset.Name(),
				resolvedPreset.DefaultSteps(),
				pc.Config,
				false,
				verbose,
				quiet,
				scaffold.WarmOptions{Source: warmSource, CopyMode: copyMode},
			); err != nil {
				ui.PrintErrorWithHint("Scaffold steps failed", err.Error())
				return err
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

func branchRefsForSelection(gitDir string) (local, remote []string, err error) {
	local, remote, err = git.GetBranchRefs(gitDir)
	if err != nil {
		return nil, nil, fmt.Errorf("listing branches: %w", err)
	}
	return local, remote, nil
}

func init() {
	addDatabaseFlags(workCmd)
	rootCmd.AddCommand(workCmd)

	workCmd.Flags().String("warm-from", "", "Reuse verified vendor/node_modules from a registered worktree")
	workCmd.Flags().String("copy-mode", "auto", "Warm dependency copy mode: auto (clone with copy fallback) or copy")
	if err := workCmd.RegisterFlagCompletionFunc("warm-from", completeWorktreeNames); err != nil {
		panic(err)
	}
	workCmd.Flags().StringP("base", "b", "", "Base branch for new worktree")
	workCmd.Flags().Bool("no-track", false, "Skip setting up remote tracking for new branches")
	workCmd.Flags().Bool("skip-scaffold", false, "Skip scaffold steps (run 'anvil scaffold' later)")
}
