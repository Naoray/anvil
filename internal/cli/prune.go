package cli

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/presets"
	"github.com/naoray/anvil/internal/scaffold"
	"github.com/naoray/anvil/internal/ui"
)

type pruneCommandDependencies struct {
	loadGlobalConfig func() (*config.GlobalConfig, error)
	openProject      func(string, string, *config.ProjectInfo, *config.GlobalConfig) (*ProjectContext, error)
	pruneProject     func(*ProjectContext, bool, bool, bool, bool, bool) error
}

func defaultPruneCommandDependencies() pruneCommandDependencies {
	return pruneCommandDependencies{
		loadGlobalConfig: config.LoadOrCreateGlobalConfig,
		openProject:      openProject,
		pruneProject: func(pc *ProjectContext, force, dryRun, verbose, quiet, keepDB bool) error {
			return pruneProject(pc, force, dryRun, verbose, quiet, keepDB)
		},
	}
}

var pruneCmd = newPruneCommand(defaultPruneCommandDependencies())

func newPruneCommand(deps pruneCommandDependencies) *cobra.Command {
	command := &cobra.Command{
		Use:   "prune",
		Short: "Remove merged worktrees across all linked projects",
		Long: `Fetches origin and removes merged worktrees for every linked project.

Lists all worktrees across all anvil-linked projects, identifies merged ones
against origin/<default-branch>, and provides an interactive review before removal.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPrune(cmd, deps)
		},
	}
	command.Flags().BoolP("force", "f", false, "Skip interactive confirmation")
	command.Flags().Bool("keep-db", false, "Keep owned databases and parallel-worker databases")
	return command
}

func runPrune(cmd *cobra.Command, deps pruneCommandDependencies) error {
	force := mustGetBool(cmd, "force")
	dryRun := mustGetBool(cmd, "dry-run")
	verbose := mustGetBool(cmd, "verbose")
	quiet := mustGetBool(cmd, "quiet")
	keepDB := mustGetBool(cmd, "keep-db")

	globalCfg, err := deps.loadGlobalConfig()
	if err != nil {
		return fmt.Errorf("loading global config: %w", err)
	}

	if len(globalCfg.Projects) == 0 {
		ui.PrintDone("No linked projects found. Run 'anvil link' first.")
		return nil
	}

	projectNames := make([]string, 0, len(globalCfg.Projects))
	for name := range globalCfg.Projects {
		projectNames = append(projectNames, name)
	}
	sort.Strings(projectNames)

	var failures []error
	for _, name := range projectNames {
		info := globalCfg.Projects[name]
		ui.PrintInfo(fmt.Sprintf("Project: %s", name))

		if info == nil {
			err := fmt.Errorf("project %q has no configuration", name)
			ui.PrintWarning(err.Error())
			failures = append(failures, err)
			fmt.Println()
			continue
		}

		pc, err := deps.openProject(info.Path, name, info, globalCfg)
		if err != nil {
			failure := fmt.Errorf("project %q: opening project: %w", name, err)
			ui.PrintWarning(failure.Error())
			failures = append(failures, failure)
			fmt.Println()
			continue
		}
		if err := deps.pruneProject(pc, force, dryRun, verbose, quiet, keepDB); err != nil {
			failure := fmt.Errorf("project %q: %w", name, err)
			ui.PrintWarning(failure.Error())
			failures = append(failures, failure)
		}
		fmt.Println()
	}

	return errors.Join(failures...)
}

type pruneProjectDependencies struct {
	fetchOrigin     func(string) error
	listWorktrees   func(string) ([]git.Worktree, error)
	isMerged        func(string, string, string) (bool, error)
	selectWorktrees func([]git.Worktree) ([]git.Worktree, error)
	confirmRemoval  func(int) (bool, error)
	removeLifecycle removeLifecycleDependencies
}

func defaultPruneProjectDependencies() pruneProjectDependencies {
	return pruneProjectDependencies{
		fetchOrigin:   git.FetchOrigin,
		listWorktrees: git.ListWorktrees,
		isMerged:      git.IsMerged,
		selectWorktrees: func(worktrees []git.Worktree) ([]git.Worktree, error) {
			return ui.SelectWorktreesToPrune(worktrees)
		},
		confirmRemoval: ui.ConfirmRemoval,
		removeLifecycle: removeLifecycleDependencies{
			readLocalState:  config.ReadLocalState,
			scaffoldManager: func(pc *ProjectContext) *scaffold.ScaffoldManager { return pc.ScaffoldManager() },
			resolvePreset: func(pc *ProjectContext, explicit, path string) presets.ResolvedPreset {
				return pc.ResolvePreset(explicit, path)
			},
			removeWorktree: git.RemoveWorktree,
		},
	}
}

// pruneProject fetches origin and removes merged worktrees for a single project.
func pruneProject(pc *ProjectContext, force, dryRun, verbose, quiet bool, keepDB ...bool) error {
	keepDatabases := false
	if len(keepDB) > 0 {
		keepDatabases = keepDB[0]
	}
	return pruneProjectWithDependencies(
		pc,
		force,
		dryRun,
		verbose,
		quiet,
		keepDatabases,
		defaultPruneProjectDependencies(),
	)
}

func pruneProjectWithDependencies(
	pc *ProjectContext,
	force, dryRun, verbose, quiet, keepDB bool,
	deps pruneProjectDependencies,
) error {
	if err := deps.fetchOrigin(pc.GitDir); err != nil {
		failure := fmt.Errorf("fetching origin: %w", err)
		ui.PrintWarning(failure.Error())
		return failure
	}

	worktrees, err := deps.listWorktrees(pc.GitDir)
	if err != nil {
		return fmt.Errorf("listing worktrees: %w", err)
	}

	remoteTarget := "origin/" + pc.DefaultBranch

	var failures []error
	var removable []git.Worktree

	for _, wt := range worktrees {
		var skippedStates []string
		if wt.Bare {
			skippedStates = append(skippedStates, "bare")
		}
		if wt.Detached {
			skippedStates = append(skippedStates, "detached")
		}
		if wt.Locked {
			skippedStates = append(skippedStates, "locked")
		}
		if len(skippedStates) > 0 {
			message := fmt.Sprintf("Skipping %s worktree at %s", strings.Join(skippedStates, " and "), wt.Path)
			if wt.LockReason != "" {
				message += fmt.Sprintf(" (%s)", wt.LockReason)
			}
			ui.PrintInfo(message)
			continue
		}
		if wt.Branch == "" {
			ui.PrintWarning(fmt.Sprintf("Skipping branchless worktree at %s", wt.Path))
			continue
		}

		if wt.Branch == pc.DefaultBranch {
			ui.PrintInfo(fmt.Sprintf("%s at %s", wt.Branch, wt.Path))
			continue
		}

		merged, err := deps.isMerged(pc.GitDir, wt.Branch, remoteTarget)
		if err != nil {
			ui.PrintErrorWithHint(fmt.Sprintf("Error checking %s", wt.Branch), err.Error())
			failures = append(failures, fmt.Errorf("worktree %q: checking merge status: %w", wt.Branch, err))
			continue
		}

		if merged {
			removable = append(removable, wt)
			ui.PrintSuccess(fmt.Sprintf("%s is merged", wt.Branch))
		} else {
			ui.PrintInfo(fmt.Sprintf("%s is not merged", wt.Branch))
		}
	}

	if len(removable) == 0 {
		if err := errors.Join(failures...); err != nil {
			return err
		}
		ui.PrintDone("No merged worktrees to remove.")
		return nil
	}

	ui.PrintInfo(fmt.Sprintf("%d merged worktree(s) found.", len(removable)))

	var toRemove []git.Worktree
	if force {
		toRemove = removable
	} else {
		selectWorktrees := deps.selectWorktrees
		if selectWorktrees == nil {
			selectWorktrees = func(worktrees []git.Worktree) ([]git.Worktree, error) {
				return ui.SelectWorktreesToPrune(worktrees)
			}
		}
		selected, err := selectWorktrees(removable)
		if err != nil {
			failures = append(failures, fmt.Errorf("selecting worktrees: %w", err))
			return errors.Join(failures...)
		}
		toRemove = selected

		if len(toRemove) == 0 {
			if err := errors.Join(failures...); err != nil {
				return err
			}
			ui.PrintInfo("No worktrees selected for removal.")
			return nil
		}

		confirmRemoval := deps.confirmRemoval
		if confirmRemoval == nil {
			confirmRemoval = ui.ConfirmRemoval
		}
		confirmed, err := confirmRemoval(len(toRemove))
		if err != nil {
			failures = append(failures, fmt.Errorf("confirmation: %w", err))
			return errors.Join(failures...)
		}
		if !confirmed {
			if err := errors.Join(failures...); err != nil {
				return err
			}
			ui.PrintInfo("No worktrees removed.")
			return nil
		}
	}

	if dryRun {
		ui.PrintInfo(fmt.Sprintf("[DRY RUN] Would remove %d worktree(s):", len(toRemove)))
	} else {
		ui.PrintInfo(fmt.Sprintf("Removing %d worktree(s):", len(toRemove)))
	}

	var removalErrors []error
	for _, wt := range toRemove {
		ui.PrintStep(fmt.Sprintf("Removing %s...", wt.Branch))

		cleanupOpts, cleanupMessages, err := planWorktreeCleanup(
			wt.Path,
			keepDB,
			dryRun,
			force,
			deps.removeLifecycle.readLocalState,
		)
		if err != nil {
			failure := fmt.Errorf("worktree %q: preparing cleanup: %w", wt.Branch, err)
			ui.PrintWarning(failure.Error())
			removalErrors = append(removalErrors, failure)
			continue
		}
		cleanupOpts.Verbose = verbose
		cleanupOpts.Quiet = quiet
		if err := runRemoveLifecycle(
			pc,
			wt,
			removeLifecycleOptions{DryRun: dryRun, Verbose: verbose, Quiet: quiet},
			cleanupOpts,
			cleanupMessages,
			deps.removeLifecycle,
		); err != nil {
			failure := fmt.Errorf("worktree %q: %w", wt.Branch, err)
			ui.PrintWarning(failure.Error())
			removalErrors = append(removalErrors, failure)
			continue
		}
		if !dryRun {
			ui.PrintSuccessPath("Removed", wt.Path)
		}
	}

	return errors.Join(append(failures, removalErrors...)...)
}

func init() {
	rootCmd.AddCommand(pruneCmd)
}
