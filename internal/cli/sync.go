package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/artisanexperiences/arbor/internal/config"
	"github.com/artisanexperiences/arbor/internal/git"
	"github.com/artisanexperiences/arbor/internal/ui"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync current worktree with upstream branch",
	Long: `Synchronizes the current worktree branch with an upstream branch by
fetching the latest changes and rebasing or merging.

The command will:
1. Fetch updates from the remote
2. Rebase (default) or merge the current branch with upstream changes

Configuration can be set via flags, project config (arbor.yaml), or interactively.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		pc, err := OpenProjectFromCWD()
		if err != nil {
			return err
		}

		// Check we're in a worktree (not project root)
		if err := pc.MustBeInWorktree(); err != nil {
			return fmt.Errorf("sync must be run from within a worktree: %w", err)
		}

		dryRun := mustGetBool(cmd, "dry-run")
		verbose := mustGetBool(cmd, "verbose")
		quiet := mustGetBool(cmd, "quiet")
		upstreamFlag := mustGetString(cmd, "upstream")
		strategyFlag := mustGetString(cmd, "strategy")
		remoteFlag := mustGetString(cmd, "remote")
		saveFlag := mustGetBool(cmd, "save")
		yesFlag := mustGetBool(cmd, "yes")

		// Get current branch
		currentBranch, err := git.GetCurrentBranch(pc.CWD)
		if err != nil {
			return fmt.Errorf("getting current branch: %w", err)
		}

		// Check for detached HEAD
		detached, err := git.IsDetachedHEAD(pc.CWD)
		if err != nil {
			return fmt.Errorf("checking HEAD status: %w", err)
		}
		if detached {
			return fmt.Errorf("cannot sync: worktree is on detached HEAD - please checkout a branch first")
		}

		// Check for rebase/merge in progress
		if git.IsRebaseInProgress(pc.CWD) {
			return fmt.Errorf("rebase in progress - resolve conflicts and run 'git rebase --continue', or run 'git rebase --abort' to cancel")
		}
		if git.IsMergeInProgress(pc.CWD) {
			return fmt.Errorf("merge in progress - resolve conflicts, stage changes, and commit, or run 'git merge --abort' to cancel")
		}

		// Check for dirty worktree
		isDirty, err := git.IsWorktreeDirty(pc.CWD)
		if err != nil {
			return fmt.Errorf("checking worktree status: %w", err)
		}
		if isDirty {
			if !quiet {
				ui.PrintInfo("Warning: worktree has uncommitted changes")
			}
			if !yesFlag && ui.IsInteractive() {
				confirmed, err := ui.Confirm("Continue with uncommitted changes?")
				if err != nil {
					return err
				}
				if !confirmed {
					return fmt.Errorf("sync aborted")
				}
			}
		}

		// Resolve upstream: CLI flag -> config -> default_branch -> interactive
		upstream := upstreamFlag
		if upstream == "" {
			upstream = pc.Config.Sync.Upstream
		}
		if upstream == "" {
			upstream = pc.DefaultBranch
		}

		// Resolve strategy: CLI flag -> config -> default (rebase)
		strategy := strategyFlag
		if strategy == "" {
			strategy = pc.Config.Sync.Strategy
		}
		if strategy == "" {
			strategy = "rebase"
		}

		// Resolve remote: CLI flag -> config -> default (origin)
		remote := remoteFlag
		if remote == "" {
			remote = pc.Config.Sync.Remote
		}
		if remote == "" {
			remote = "origin"
		}

		// Validate strategy
		if strategy != "rebase" && strategy != "merge" {
			return fmt.Errorf("invalid strategy %q: must be 'rebase' or 'merge'", strategy)
		}

		// Interactive prompts if needed and allowed
		shouldPrompt := !yesFlag && ui.ShouldPrompt(cmd, upstreamFlag != "" || pc.Config.Sync.Upstream != "")
		if shouldPrompt {
			// Prompt for upstream if not set via flag or config
			if upstreamFlag == "" && pc.Config.Sync.Upstream == "" {
				localBranches, err := git.ListLocalBranches(pc.BarePath)
				if err != nil {
					return fmt.Errorf("listing local branches: %w", err)
				}

				remoteBranches, _ := git.ListRemoteBranches(pc.BarePath)

				selected, err := ui.SelectUpstreamBranch(localBranches, remoteBranches, pc.DefaultBranch)
				if err != nil {
					return fmt.Errorf("selecting upstream branch: %w", err)
				}
				upstream = selected
			}

			// Prompt for strategy if not set via flag or config
			if strategyFlag == "" && pc.Config.Sync.Strategy == "" {
				selected, err := ui.SelectSyncStrategy(strategy)
				if err != nil {
					return fmt.Errorf("selecting strategy: %w", err)
				}
				strategy = selected
			}

			// Confirm operation
			confirmed, err := ui.ConfirmSync(currentBranch, upstream, strategy)
			if err != nil {
				return err
			}
			if !confirmed {
				return fmt.Errorf("sync aborted")
			}
		}

		// Validate upstream is provided in non-interactive mode
		if upstream == "" {
			return fmt.Errorf("upstream branch required - provide --upstream flag, set sync.upstream in arbor.yaml, or run interactively")
		}

		// Check remote exists
		remoteURL, err := git.GetRemoteURL(pc.BarePath, remote)
		if err != nil {
			return fmt.Errorf("checking remote: %w", err)
		}
		if remoteURL == "" {
			return fmt.Errorf("remote %q not configured - add it with 'git remote add %s <url>'", remote, remote)
		}

		// Print info
		if !quiet {
			ui.PrintStep(fmt.Sprintf("Syncing branch '%s' with '%s/%s' using %s", currentBranch, remote, upstream, strategy))
		}

		if dryRun {
			ui.PrintInfo(fmt.Sprintf("[DRY RUN] Would fetch from %s", remote))
			ui.PrintInfo(fmt.Sprintf("[DRY RUN] Would %s %s/%s into %s", strategy, remote, upstream, currentBranch))
			ui.PrintDone("Dry run complete")
			return nil
		}

		// Fetch remote
		if verbose && !quiet {
			ui.PrintInfo(fmt.Sprintf("Fetching from %s", remote))
		}
		if err := git.FetchRemote(pc.BarePath, remote); err != nil {
			return fmt.Errorf("fetch failed: %w", err)
		}
		if !quiet {
			ui.PrintSuccess(fmt.Sprintf("Fetched from %s", remote))
		}

		// Run rebase or merge
		if !quiet {
			ui.PrintInfo(fmt.Sprintf("Running %s %s/%s...", strategy, remote, upstream))
		}

		var syncErr error
		if strategy == "rebase" {
			syncErr = git.RebaseOnto(pc.CWD, remote, upstream)
		} else {
			syncErr = git.MergeInto(pc.CWD, remote, upstream)
		}

		if syncErr != nil {
			return syncErr
		}

		if !quiet {
			ui.PrintSuccess(fmt.Sprintf("Successfully synced with %s/%s using %s", remote, upstream, strategy))
		}

		// Save config if requested
		shouldSave := saveFlag
		if !saveFlag && shouldPrompt {
			// Prompt to save if not already configured
			if pc.Config.Sync.Upstream == "" || pc.Config.Sync.Strategy == "" {
				saveConfirmed, err := ui.ConfirmSaveSyncConfig()
				if err != nil {
					return err
				}
				shouldSave = saveConfirmed
			}
		}

		if shouldSave {
			pc.Config.Sync = config.SyncConfig{
				Upstream: upstream,
				Strategy: strategy,
				Remote:   remote,
			}
			if err := config.SaveProject(pc.ProjectPath, pc.Config); err != nil {
				ui.PrintError(fmt.Sprintf("Failed to save sync config: %v", err))
			} else {
				ui.PrintSuccess("Saved sync settings to arbor.yaml")
			}
		}

		ui.PrintDone(fmt.Sprintf("Branch '%s' is now in sync with '%s/%s'", currentBranch, remote, upstream))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(syncCmd)

	syncCmd.Flags().StringP("upstream", "u", "", "Upstream branch to sync against (e.g., main)")
	syncCmd.Flags().StringP("strategy", "s", "", "Sync strategy: rebase or merge (default: rebase)")
	syncCmd.Flags().StringP("remote", "r", "", "Remote name to fetch from (default: origin)")
	syncCmd.Flags().Bool("save", false, "Persist sync settings to arbor.yaml")
	syncCmd.Flags().BoolP("yes", "y", false, "Skip confirmations and run with chosen values")
}
