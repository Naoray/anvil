package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/artisanexperiences/arbor/internal/config"
	"github.com/artisanexperiences/arbor/internal/git"
	"github.com/artisanexperiences/arbor/internal/presets"
	"github.com/artisanexperiences/arbor/internal/scaffold"
	"github.com/artisanexperiences/arbor/internal/scaffold/steps"
	"github.com/artisanexperiences/arbor/internal/ui"
)

var destroyCmd = &cobra.Command{
	Use:   "destroy [PROJECT_PATH]",
	Short: "Completely destroy an arbor project",
	Long: `Destroys an arbor project by:
  1. Finding all worktrees
  2. Running cleanup for each (features first, then main)
  3. Removing all worktrees and branches
  4. Deleting the project folder

This operation cannot be undone.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun := mustGetBool(cmd, "dry-run")
		verbose := mustGetBool(cmd, "verbose")
		quiet := mustGetBool(cmd, "quiet")
		force := mustGetBool(cmd, "force")

		var projectPath string
		if len(args) > 0 {
			projectPath = args[0]
		} else {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting current directory: %w", err)
			}
			selected, err := ui.SelectProjectToDestroy(cwd)
			if err != nil {
				return err
			}
			projectPath = selected
		}

		absProjectPath, err := filepath.Abs(projectPath)
		if err != nil {
			return fmt.Errorf("resolving path: %w", err)
		}

		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting current directory: %w", err)
		}
		if cwd == absProjectPath || strings.HasPrefix(cwd, absProjectPath+string(filepath.Separator)) {
			return fmt.Errorf("cannot destroy project from within it; cd out first")
		}

		cfg, err := config.LoadProject(absProjectPath)
		if err != nil {
			return fmt.Errorf("not an arbor project: %w", err)
		}

		barePath := filepath.Join(absProjectPath, ".bare")
		if _, err := os.Stat(barePath); err != nil {
			return fmt.Errorf("project missing .bare folder: %w", err)
		}

		worktrees, err := git.ListWorktrees(barePath)
		if err != nil {
			return fmt.Errorf("listing worktrees: %w", err)
		}
		worktrees = sortWorktreesForDestroy(worktrees, cfg.DefaultBranch)

		projectName := cfg.SiteName
		if projectName == "" {
			projectName = filepath.Base(absProjectPath)
		}

		if !force && !dryRun {
			confirmed, err := ui.ConfirmDestroy(projectName, worktrees)
			if err != nil {
				return err
			}
			if !confirmed {
				ui.PrintInfo("Cancelled.")
				return nil
			}
		}

		if dryRun {
			ui.PrintInfo(fmt.Sprintf("Would destroy project %q with %d worktrees:", projectName, len(worktrees)))
			for _, wt := range worktrees {
				ui.PrintInfo(fmt.Sprintf("  - %s", wt.Branch))
			}
			return nil
		}

		preset := cfg.Preset
		presetManager := presets.NewManager()

		// Create explicit step registry with default steps
		stepRegistry := steps.NewRegistry()
		stepRegistry.RegisterDefaults()
		scaffoldManager := scaffold.NewScaffoldManagerWithRegistry(stepRegistry)
		presets.RegisterAllWithScaffold(scaffoldManager)

		allCleanupFailed := true
		repoName := filepath.Base(absProjectPath)
		for _, wt := range worktrees {
			ui.PrintStep("Removing worktree: " + wt.Branch)

			wtPreset := preset
			if wtPreset == "" {
				wtPreset = presetManager.Detect(wt.Path)
			}

			if wtPreset != "" {
				siteName := filepath.Base(wt.Path)
				if wt.Branch == cfg.DefaultBranch && cfg.SiteName != "" {
					siteName = cfg.SiteName
				}
				if err := scaffoldManager.RunCleanup(wt.Path, wt.Branch, repoName, siteName, wtPreset, cfg, false, verbose, quiet); err != nil {
					ui.PrintWarning(fmt.Sprintf("Cleanup failed for %s: %v", wt.Branch, err))
				} else {
					allCleanupFailed = false
				}
			} else {
				allCleanupFailed = false
			}

			if err := git.RemoveWorktree(wt.Path, true); err != nil {
				ui.PrintWarning(fmt.Sprintf("Failed to remove worktree %s: %v", wt.Branch, err))
			}

			if err := git.DeleteBranch(barePath, wt.Branch, true); err != nil {
				ui.PrintWarning(fmt.Sprintf("Failed to delete branch %s: %v", wt.Branch, err))
			}

			ui.PrintSuccess(fmt.Sprintf("Removed %s", wt.Branch))
		}

		if allCleanupFailed && len(worktrees) > 0 {
			ui.PrintWarning("All cleanup steps failed. This may indicate a serious issue. Aborting.")
			return fmt.Errorf("all cleanup operations failed")
		}

		if err := git.PruneWorktrees(barePath); err != nil {
			ui.PrintWarning(fmt.Sprintf("Failed to prune worktrees: %v", err))
		}

		ui.PrintStep("Deleting project folder...")
		if err := os.RemoveAll(absProjectPath); err != nil {
			return fmt.Errorf("deleting project folder: %w", err)
		}

		ui.PrintDone(fmt.Sprintf("Destroyed project %q", projectName))
		return nil
	},
}

func sortWorktreesForDestroy(worktrees []git.Worktree, defaultBranch string) []git.Worktree {
	sort.SliceStable(worktrees, func(i, j int) bool {
		iIsMain := worktrees[i].Branch == defaultBranch
		jIsMain := worktrees[j].Branch == defaultBranch
		if iIsMain != jIsMain {
			return !iIsMain
		}
		return worktrees[i].Branch < worktrees[j].Branch
	})
	return worktrees
}

func init() {
	rootCmd.AddCommand(destroyCmd)
	destroyCmd.Flags().BoolP("force", "f", false, "Skip confirmation prompt")
}
