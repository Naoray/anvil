package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/git"
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
			if err := pruneProject(pc, force, dryRun, verbose, quiet); err != nil {
				ui.PrintWarning(fmt.Sprintf("Error pruning %s: %v", name, err))
			}
			fmt.Println()
		}

		return nil
	},
}

// pruneProject fetches origin and removes merged worktrees for a single project.
func pruneProject(pc *ProjectContext, force, dryRun, verbose, quiet bool) error {
	if err := git.FetchOrigin(pc.GitDir); err != nil {
		ui.PrintWarning(fmt.Sprintf("Could not fetch origin: %v", err))
	}

	worktrees, err := git.ListWorktrees(pc.GitDir)
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

		merged, err := git.IsMerged(pc.GitDir, wt.Branch, remoteTarget)
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

		if !dryRun {
			preset := pc.Config.Preset
			if preset == "" {
				preset = pc.PresetManager().Detect(wt.Path)
			}

			siteName := filepath.Base(wt.Path)
			if err := pc.ScaffoldManager().RunCleanup(wt.Path, wt.Branch, "", siteName, preset, pc.Config, false, verbose, quiet); err != nil {
				ui.PrintErrorWithHint("Cleanup failed", err.Error())
			}

			if err := git.RemoveWorktree(pc.GitDir, wt.Path, true); err != nil {
				ui.PrintErrorWithHint(fmt.Sprintf("Error removing %s", wt.Branch), err.Error())
			}
		} else {
			ui.PrintInfo(fmt.Sprintf("[DRY RUN] Would remove %s and run cleanup", wt.Branch))
		}
	}

	return nil
}

func init() {
	rootCmd.AddCommand(pruneCmd)

	pruneCmd.Flags().BoolP("force", "f", false, "Skip interactive confirmation")
}
