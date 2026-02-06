package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/ui"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync current worktree with upstream branch",
	Long: `Synchronizes the current worktree branch with an upstream branch by
fetching the latest changes and rebasing or merging.

The command will:
1. Auto-stash changes (tracked modifications and untracked files) by default
2. Fetch updates from the remote
3. Rebase (default) or merge the current branch with upstream changes
4. Restore stashed changes after successful sync

Note: Ignored files (node_modules, vendor, etc.) are not stashed for performance,
as they are not modified by git during sync anyway.

Auto-stashing can be disabled with --no-auto-stash flag or by setting
sync.auto_stash: false in anvil.yaml.

Configuration can be set via flags, project config (anvil.yaml), or interactively.`,
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
		noAutoStashFlag := mustGetBool(cmd, "no-auto-stash")

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

		// Determine if auto-stash should be used
		// Priority: CLI flag > config > default (true)
		autoStash := true
		if noAutoStashFlag {
			autoStash = false
		} else if pc.Config.Sync.AutoStash != nil {
			autoStash = *pc.Config.Sync.AutoStash
		}

		// Check for any changes (tracked, untracked, ignored)
		hasChanges, err := git.HasChanges(pc.CWD)
		if err != nil {
			return fmt.Errorf("checking for changes: %w", err)
		}

		// Track whether we created a stash so we can pop it later
		var stashCreated bool

		if hasChanges && !autoStash {
			// Auto-stash disabled but there are changes - warn the user
			if !quiet {
				ui.PrintWarning("Warning: worktree has changes (auto-stash is disabled)")
				ui.PrintInfo("Untracked files that conflict with upstream may be lost")
			}
			if !yesFlag && ui.IsInteractive() {
				confirmed, err := ui.Confirm("Continue without stashing changes?")
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
				localBranches, err := git.ListLocalBranches(pc.GitDir)
				if err != nil {
					return fmt.Errorf("listing local branches: %w", err)
				}

				remoteBranches, _ := git.ListRemoteBranches(pc.GitDir)

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
			return fmt.Errorf("upstream branch required - provide --upstream flag, set sync.upstream in anvil.yaml, or run interactively")
		}

		// Check remote exists
		remoteURL, err := git.GetRemoteURL(pc.GitDir, remote)
		if err != nil {
			return fmt.Errorf("checking remote: %w", err)
		}
		if remoteURL == "" {
			return fmt.Errorf("remote %q not configured - add it with 'git remote add %s <url>'", remote, remote)
		}

		if hasChanges && autoStash {
			if !quiet {
				ui.PrintInfo("Auto-stashing changes (tracked modifications and untracked files)...")
			}

			if !dryRun {
				if err := git.StashAll(pc.CWD, "anvil sync auto-stash"); err != nil {
					return fmt.Errorf("failed to stash changes: %w", err)
				}
				stashCreated = true
				if !quiet {
					ui.PrintSuccess("Changes stashed successfully")
				}
			} else {
				ui.PrintInfo("[DRY RUN] Would stash tracked and untracked changes")
			}
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
		if err := git.FetchRemote(pc.GitDir, remote); err != nil {
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
			// Leave stash intact on sync failure
			if stashCreated && !quiet {
				ui.PrintInfo("\nYour changes are preserved in the stash.")
				ui.PrintInfo("After fixing the issue, run 'git stash pop' to restore them.")
			}
			return syncErr
		}

		if !quiet {
			ui.PrintSuccess(fmt.Sprintf("Successfully synced with %s/%s using %s", remote, upstream, strategy))
		}

		// Pop the stash after successful sync
		if stashCreated && !dryRun {
			if verbose && !quiet {
				ui.PrintInfo("Restoring stashed changes...")
			}

			popErr := git.PopStash(pc.CWD)
			if popErr != nil {
				// Check if it's a conflict error
				if _, isConflict := popErr.(*git.StashConflictError); isConflict {
					ui.PrintWarning("\nWarning: Could not automatically restore stashed changes due to conflicts")
					ui.PrintInfo("\nYour changes have been safely preserved in the stash.")
					ui.PrintInfo("To restore them, resolve conflicts and run:")
					ui.PrintInfo("  git stash pop")
					ui.PrintInfo("\nTo discard the stash:")
					ui.PrintInfo("  git stash drop")
				} else {
					ui.PrintWarning(fmt.Sprintf("\nWarning: Failed to restore stashed changes: %v", popErr))
					ui.PrintInfo("Your changes are still in the stash. Run 'git stash pop' to restore them manually.")
				}
			} else {
				if !quiet {
					ui.PrintSuccess("Stashed changes restored successfully")
				}
			}
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
				Upstream:  upstream,
				Strategy:  strategy,
				Remote:    remote,
				AutoStash: &autoStash,
			}
			if err := config.SaveProject(pc.ProjectPath, pc.Config); err != nil {
				ui.PrintError(fmt.Sprintf("Failed to save sync config: %v", err))
			} else {
				ui.PrintSuccess("Saved sync settings to anvil.yaml")
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
	syncCmd.Flags().Bool("save", false, "Persist sync settings to anvil.yaml")
	syncCmd.Flags().BoolP("yes", "y", false, "Skip confirmations and run with chosen values")
	syncCmd.Flags().Bool("no-auto-stash", false, "Disable automatic stashing of all changes before sync")
}
