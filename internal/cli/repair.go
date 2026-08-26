package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/ui"
)

type repairCommandDependencies struct {
	openProject          func() (*ProjectContext, error)
	repairFetchRefspec   func(*ProjectContext, bool, bool) error
	repairBranchTracking func(*ProjectContext, bool, bool) error
}

func defaultRepairCommandDependencies() repairCommandDependencies {
	return repairCommandDependencies{
		openProject:          OpenProjectFromCWD,
		repairFetchRefspec:   repairFetchRefspec,
		repairBranchTracking: repairBranchTracking,
	}
}

var repairCmd = newRepairCommand(defaultRepairCommandDependencies())

func newRepairCommand(deps repairCommandDependencies) *cobra.Command {
	command := &cobra.Command{
		Use:   "repair",
		Short: "Repair git configuration for existing anvil project",
		Long: `Fixes fetch refspec and branch tracking configuration for an existing anvil project.

Use this command if:
- Fetch refspec was not configured
- You need to reset remote configuration
- Branch tracking needs to be fixed

This will:
1. Configure fetch refspec for the repository (unless --tracking-only)
2. Set up tracking for all local branches that don't have it (unless --refspec-only)

This command is idempotent and safe to run multiple times.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			pc, err := deps.openProject()
			if err != nil {
				return err
			}

			dryRun := mustGetBool(cmd, "dry-run")
			verbose := mustGetBool(cmd, "verbose")
			refspecOnly := mustGetBool(cmd, "refspec-only")
			trackingOnly := mustGetBool(cmd, "tracking-only")

			if refspecOnly && trackingOnly {
				return fmt.Errorf("cannot use --refspec-only and --tracking-only together")
			}

			// Phase 1: Fix fetch refspec
			if !trackingOnly {
				if err := deps.repairFetchRefspec(pc, dryRun, verbose); err != nil {
					return err
				}
			}

			// Phase 2: Fix branch tracking
			if !refspecOnly {
				if err := deps.repairBranchTracking(pc, dryRun, verbose); err != nil {
					return err
				}
			}

			ui.PrintDone("Repair complete")
			return nil
		},
	}
	command.Flags().Bool("dry-run", false, "Show what would be done without making changes")
	command.Flags().Bool("refspec-only", false, "Only repair fetch refspec, skip branch tracking")
	command.Flags().Bool("tracking-only", false, "Only repair branch tracking, skip fetch refspec")

	return command
}

func repairFetchRefspec(pc *ProjectContext, dryRun, verbose bool) error {
	// Check if already configured
	hasRefspec, err := git.HasFetchRefspec(pc.GitDir)
	if err != nil {
		return fmt.Errorf("checking fetch refspec: %w", err)
	}

	if hasRefspec {
		if verbose {
			ui.PrintInfo("Fetch refspec already configured")
		}
		return nil
	}

	// Try to get remote URL from bare repo config
	remoteURL, err := git.GetRemoteURL(pc.GitDir, config.DefaultRemote)
	if err != nil {
		return fmt.Errorf("getting remote URL: %w", err)
	}

	// If not in bare repo, try to get from a worktree
	if remoteURL == "" {
		worktrees, err := git.ListWorktrees(pc.GitDir)
		if err != nil {
			return fmt.Errorf("listing worktrees: %w", err)
		}

		remoteURL = remoteURLFromWorktrees(worktrees, git.GetRemoteURLFromWorktree)
	}

	// If still no URL, prompt user
	if remoteURL == "" {
		if ui.IsInteractive() {
			var promptedURL string
			form := huh.NewForm(
				huh.NewGroup(
					huh.NewInput().
						Title("Enter remote URL for origin").
						Placeholder("git@github.com:user/repo.git").
						Value(&promptedURL),
				),
			).WithTheme(huh.ThemeCatppuccin())

			if err := form.Run(); err != nil {
				return fmt.Errorf("prompting for remote URL: %w", ui.NormalizeAbort(err))
			}
			remoteURL = promptedURL
		} else {
			return fmt.Errorf("remote URL not configured and not running interactively - provide URL via other means")
		}
	} else {
		// Confirm with user if we found a URL
		if ui.IsInteractive() {
			confirmed, newURL, err := confirmOrEditURL(
				fmt.Sprintf("Found remote URL: %s", remoteURL),
				remoteURL,
			)
			if err != nil {
				return fmt.Errorf("confirming remote URL: %w", err)
			}
			if !confirmed {
				ui.PrintInfo("Skipping fetch refspec configuration")
				return nil
			}
			remoteURL = newURL
		} else {
			// Non-interactive: use the found URL
			ui.PrintInfo(fmt.Sprintf("Using found remote URL: %s", remoteURL))
		}
	}

	if dryRun {
		ui.PrintInfo(fmt.Sprintf("[DRY RUN] Would configure fetch refspec for %s", remoteURL))
		return nil
	}

	if err := git.ConfigureFetchRefspec(pc.GitDir, remoteURL); err != nil {
		return fmt.Errorf("configuring fetch refspec: %w", err)
	}
	ui.PrintSuccess("Configured fetch refspec")

	return nil
}

func confirmOrEditURL(message, currentValue string) (bool, string, error) {
	var action string
	options := []huh.Option[string]{
		huh.NewOption("Confirm and use this URL", "confirm"),
		huh.NewOption("Edit URL", "edit"),
		huh.NewOption("Skip this step", "skip"),
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(message).
				Options(options...).
				Value(&action),
		),
	).WithTheme(huh.ThemeCatppuccin())

	if err := form.Run(); err != nil {
		return false, "", ui.NormalizeAbort(err)
	}

	switch action {
	case "confirm":
		return true, currentValue, nil
	case "skip":
		return false, "", nil
	case "edit":
		var newURL string
		editForm := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Enter new remote URL").
					Placeholder(currentValue).
					Value(&newURL),
			),
		).WithTheme(huh.ThemeCatppuccin())

		if err := editForm.Run(); err != nil {
			return false, "", ui.NormalizeAbort(err)
		}
		if newURL == "" {
			newURL = currentValue
		}
		return true, newURL, nil
	}

	return false, "", nil
}

func remoteURLFromWorktrees(worktrees []git.Worktree, getRemoteURL func(string) (string, error)) string {
	for _, wt := range worktrees {
		if wt.Bare {
			continue
		}
		url, err := getRemoteURL(wt.Path)
		if err == nil && url != "" {
			return url
		}
	}
	return ""
}

type repairBranchTrackingDependencies struct {
	getBranchRefs     func(string) ([]string, []string, error)
	hasBranchTracking func(string, string) (bool, error)
	setBranchUpstream func(string, string, string) error
}

func defaultRepairBranchTrackingDependencies() repairBranchTrackingDependencies {
	return repairBranchTrackingDependencies{
		getBranchRefs:     git.GetBranchRefs,
		hasBranchTracking: git.HasBranchTracking,
		setBranchUpstream: git.SetBranchUpstream,
	}
}

func repairBranchTracking(pc *ProjectContext, dryRun, verbose bool) error {
	return repairBranchTrackingWithDependencies(pc, dryRun, verbose, defaultRepairBranchTrackingDependencies())
}

func repairBranchTrackingWithDependencies(pc *ProjectContext, dryRun, verbose bool, deps repairBranchTrackingDependencies) error {
	localBranches, remoteBranches, err := deps.getBranchRefs(pc.GitDir)
	if err != nil {
		return fmt.Errorf("listing branches: %w", err)
	}

	// Build set of remote branch names (without origin/ prefix) for quick lookup
	remoteSet := make(map[string]bool)
	for _, rb := range remoteBranches {
		// Strip "origin/" prefix
		if name := strings.TrimPrefix(rb, "origin/"); name != rb {
			remoteSet[name] = true
		}
	}

	fixed := 0
	skipped := 0
	var failures []error

	for _, branch := range localBranches {
		hasTracking, err := deps.hasBranchTracking(pc.GitDir, branch)
		if err != nil {
			failure := fmt.Errorf("branch %q: checking tracking: %w", branch, err)
			failures = append(failures, failure)
			if verbose {
				ui.PrintWarning(failure.Error())
			}
			continue
		}

		if hasTracking {
			skipped++
			if verbose {
				ui.PrintInfo(fmt.Sprintf("Branch '%s' already has tracking", branch))
			}
			continue
		}

		// Check if corresponding remote branch exists
		if !remoteSet[branch] {
			if verbose {
				ui.PrintInfo(fmt.Sprintf("No remote branch for '%s', skipping tracking setup", branch))
			}
			continue
		}

		if dryRun {
			ui.PrintInfo(fmt.Sprintf("[DRY RUN] Would set up tracking for branch '%s'", branch))
			fixed++
			continue
		}

		if err := deps.setBranchUpstream(pc.GitDir, branch, config.DefaultRemote); err != nil {
			failure := fmt.Errorf("branch %q: setting upstream: %w", branch, err)
			failures = append(failures, failure)
			ui.PrintWarning(failure.Error())
			continue
		}

		ui.PrintSuccess(fmt.Sprintf("Set up tracking for branch '%s'", branch))
		fixed++
	}

	if len(failures) == 0 {
		if fixed == 0 && skipped > 0 {
			ui.PrintInfo("All branches already have tracking configured")
		} else if fixed == 0 {
			ui.PrintInfo("No branches needed tracking configuration")
		}
	}

	return errors.Join(failures...)
}

func init() {
	rootCmd.AddCommand(repairCmd)
}
