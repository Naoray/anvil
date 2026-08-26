package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/scaffold"
	"github.com/naoray/anvil/internal/ui"
)

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Remove merged worktrees across all linked projects",
	Long: `Fetches origin and removes merged worktrees for every linked project.

Lists all worktrees across all anvil-linked projects, identifies merged ones
against origin/<default-branch>, and provides an interactive review before removal.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		force := mustGetBool(cmd, "force")
		dryRun := mustGetBool(cmd, "dry-run")
		verbose := mustGetBool(cmd, "verbose")
		quiet := mustGetBool(cmd, "quiet")
		keepDB := mustGetBool(cmd, "keep-db")

		globalCfg, err := config.LoadOrCreateGlobalConfig()
		if err != nil {
			return fmt.Errorf("loading global config: %w", err)
		}

		if len(globalCfg.Projects) == 0 {
			ui.PrintDone("No linked projects found. Run 'anvil link' first.")
			return nil
		}

		for name, info := range globalCfg.Projects {
			ui.PrintInfo(fmt.Sprintf("Project: %s", name))
			pc, err := openProject(info.Path, name, info, globalCfg)
			if err != nil {
				ui.PrintWarning(fmt.Sprintf("Skipping %s: %v", name, err))
				continue
			}
			if err := pruneProject(pc, force, dryRun, verbose, quiet, keepDB); err != nil {
				ui.PrintWarning(fmt.Sprintf("Error pruning %s: %v", name, err))
			}
			fmt.Println()
		}

		return nil
	},
}

type pruneProjectDependencies struct {
	fetchOrigin     func(string) error
	listWorktrees   func(string) ([]git.Worktree, error)
	isMerged        func(string, string, string) (bool, error)
	removeLifecycle removeLifecycleDependencies
}

func defaultPruneProjectDependencies() pruneProjectDependencies {
	return pruneProjectDependencies{
		fetchOrigin:   git.FetchOrigin,
		listWorktrees: git.ListWorktrees,
		isMerged:      git.IsMerged,
		removeLifecycle: removeLifecycleDependencies{
			readLocalState:  config.ReadLocalState,
			scaffoldManager: func(pc *ProjectContext) *scaffold.ScaffoldManager { return pc.ScaffoldManager() },
			detectPreset:    func(pc *ProjectContext, path string) string { return pc.PresetManager().Detect(path) },
			removeWorktree:  git.RemoveWorktree,
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
		ui.PrintWarning(fmt.Sprintf("Could not fetch origin: %v", err))
	}

	worktrees, err := deps.listWorktrees(pc.GitDir)
	if err != nil {
		return fmt.Errorf("listing worktrees: %w", err)
	}

	remoteTarget := "origin/" + pc.DefaultBranch

	var removable []git.Worktree

	for _, wt := range worktrees {
		if wt.Branch == pc.DefaultBranch || wt.Branch == "(bare)" {
			ui.PrintInfo(fmt.Sprintf("%s at %s", wt.Branch, wt.Path))
			continue
		}

		merged, err := deps.isMerged(pc.GitDir, wt.Branch, remoteTarget)
		if err != nil {
			ui.PrintErrorWithHint(fmt.Sprintf("Error checking %s", wt.Branch), err.Error())
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
		ui.PrintDone("No merged worktrees to remove.")
		return nil
	}

	ui.PrintInfo(fmt.Sprintf("%d merged worktree(s) found.", len(removable)))

	var toRemove []git.Worktree
	if force {
		toRemove = removable
	} else {
		selected, err := ui.SelectWorktreesToPrune(removable)
		if err != nil {
			return fmt.Errorf("selecting worktrees: %w", err)
		}
		toRemove = selected

		if len(toRemove) == 0 {
			ui.PrintInfo("No worktrees selected for removal.")
			return nil
		}

		confirmed, err := ui.ConfirmRemoval(len(toRemove))
		if err != nil {
			return fmt.Errorf("confirmation: %w", err)
		}
		if !confirmed {
			ui.PrintInfo("No worktrees removed.")
			return nil
		}
	}

	ui.PrintInfo(fmt.Sprintf("Removing %d worktree(s):", len(toRemove)))
	for _, wt := range toRemove {
		ui.PrintSuccessPath("Removed", wt.Path)
	}

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
			return fmt.Errorf("preparing cleanup for %s: %w", wt.Branch, err)
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
			return fmt.Errorf("removing %s: %w", wt.Branch, err)
		}
	}

	return nil
}

func init() {
	rootCmd.AddCommand(pruneCmd)

	pruneCmd.Flags().BoolP("force", "f", false, "Skip interactive confirmation")
	pruneCmd.Flags().Bool("keep-db", false, "Keep owned databases and parallel-worker databases")
}
