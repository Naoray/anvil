package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/michaeldyrynda/arbor/internal/git"
	"github.com/michaeldyrynda/arbor/internal/ui"
)

var scaffoldCmd = &cobra.Command{
	Use:   "scaffold [PATH]",
	Short: "Run scaffold steps for a worktree",
	Long: `Run scaffold steps for an existing worktree.

When run from the project root (where .bare is located), you can specify a worktree
path relative to the project root (e.g., 'main', 'feature/my-feature').

When run from inside a worktree without arguments, you'll be prompted to confirm
scaffolding the current worktree.

If no path is provided and not inside a worktree, you can interactively select
a worktree to scaffold.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		pc, err := OpenProjectFromCWD()
		if err != nil {
			return fmt.Errorf("opening project: %w", err)
		}

		dryRun := mustGetBool(cmd, "dry-run")
		verbose := mustGetBool(cmd, "verbose")

		worktrees, err := git.ListWorktreesDetailed(pc.BarePath, pc.CWD, pc.DefaultBranch)
		if err != nil {
			return fmt.Errorf("listing worktrees: %w", err)
		}

		if len(worktrees) == 0 {
			return fmt.Errorf("no worktrees found in project")
		}

		var selectedWorktree *git.Worktree

		if len(args) > 0 {
			worktreePath := args[0]

			if !filepath.IsAbs(worktreePath) {
				worktreePath = filepath.Join(pc.ProjectPath, worktreePath)
			}

			absWorktreePath, err := filepath.Abs(worktreePath)
			if err != nil {
				return fmt.Errorf("getting absolute path: %w", err)
			}

			for _, wt := range worktrees {
				wtAbsPath, err := filepath.Abs(wt.Path)
				if err != nil {
					continue
				}
				if wtAbsPath == absWorktreePath {
					selectedWorktree = &wt
					break
				}
			}

			if selectedWorktree == nil {
				return fmt.Errorf("worktree not found: %s", worktreePath)
			}
		} else if pc.IsInWorktree() {
			for _, wt := range worktrees {
				wtAbsPath, _ := filepath.Abs(wt.Path)
				projectRootAbsPath, _ := filepath.Abs(pc.ProjectPath)

				if filepath.Dir(wtAbsPath) == projectRootAbsPath {
					if wt.IsCurrent {
						selectedWorktree = &wt
						break
					}
				}
			}

			if selectedWorktree == nil {
				return fmt.Errorf("current worktree not found")
			}

			if ui.IsInteractive() {
				confirmed, err := ui.ConfirmScaffold(selectedWorktree.Branch)
				if err != nil {
					return err
				}
				if !confirmed {
					ui.PrintInfo("Scaffold cancelled")
					return nil
				}
			}
		} else {
			if !ui.IsInteractive() {
				return fmt.Errorf("worktree path required (run from project root with path, or use interactive mode)")
			}

			selected, err := ui.SelectWorktreeToScaffold(worktrees)
			if err != nil {
				return err
			}
			selectedWorktree = selected
		}

		if selectedWorktree == nil {
			return fmt.Errorf("no worktree selected")
		}

		ui.PrintStep(fmt.Sprintf("Scaffolding worktree: %s", selectedWorktree.Branch))
		ui.PrintInfo(fmt.Sprintf("Path: %s", selectedWorktree.Path))

		preset := pc.Config.Preset
		if preset == "" {
			preset = pc.PresetManager().Detect(selectedWorktree.Path)
		}

		if verbose && preset != "" {
			ui.PrintInfo(fmt.Sprintf("Running scaffold for preset: %s", preset))
		}

		repoName := filepath.Base(pc.ProjectPath)
		worktreeName := filepath.Base(selectedWorktree.Path)

		// For the default branch, use the saved SiteName from project config
		// For feature branches, use the worktree folder name
		siteName := worktreeName
		if selectedWorktree.Branch == pc.DefaultBranch && pc.Config.SiteName != "" {
			siteName = pc.Config.SiteName
		}

		if err := pc.ScaffoldManager().RunScaffold(selectedWorktree.Path, selectedWorktree.Branch, repoName, siteName, preset, pc.Config, dryRun, verbose); err != nil {
			ui.PrintErrorWithHint("Scaffold steps failed", err.Error())
			return err
		}

		ui.PrintDone(fmt.Sprintf("Scaffold complete: %s", selectedWorktree.Branch))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(scaffoldCmd)
}
