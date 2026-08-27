package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/ui"
)

var scaffoldCmd = &cobra.Command{
	Use:   "scaffold [WORKTREE]",
	Short: "Run scaffold steps for a worktree",
	Long: `Run scaffold steps for an existing worktree.

Arguments:
  WORKTREE  Name of the worktree (folder name, branch name, or partial match)
            If omitted and inside a worktree, scaffolds the current worktree.
            If omitted and not inside a worktree, prompts for selection.

Examples:
  anvil scaffold feature-auth       # Scaffold by folder name
  anvil scaffold auth               # Partial match
  anvil scaffold feature/auth       # Match by branch name`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completeWorktreeNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		pc, err := OpenProjectFromCWD()
		if err != nil {
			return fmt.Errorf("opening project: %w", err)
		}

		dryRun := mustGetBool(cmd, "dry-run")
		verbose := mustGetBool(cmd, "verbose")
		quiet := mustGetBool(cmd, "quiet")

		worktrees, err := git.ListWorktreesDetailed(pc.GitDir, pc.CWD, pc.DefaultBranch)
		if err != nil {
			return fmt.Errorf("listing worktrees: %w", err)
		}

		if len(worktrees) == 0 {
			return fmt.Errorf("no worktrees found in project")
		}

		var selectedWorktree *git.Worktree

		if len(args) > 0 {
			resolvedPath, err := findWorktreePath(pc.GitDir, args[0])
			if err != nil {
				return err
			}

			absResolved, err := filepath.Abs(resolvedPath)
			if err != nil {
				return fmt.Errorf("getting absolute path: %w", err)
			}

			for _, wt := range worktrees {
				wtAbsPath, err := filepath.Abs(wt.Path)
				if err != nil {
					continue
				}
				if git.PathsEqual(wtAbsPath, absResolved) {
					selectedWorktree = &wt
					break
				}
			}

			if selectedWorktree == nil {
				return fmt.Errorf("worktree not found: %s", resolvedPath)
			}
		} else if pc.IsInWorktree() {
			selectedWorktree, err = selectWorktreeByContainment(worktrees, pc.CWD)
			if err != nil {
				return fmt.Errorf("finding current worktree: %w", err)
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

		resolvedPreset := pc.ResolvePreset("", selectedWorktree.Path)

		if verbose && resolvedPreset.Name() != "" {
			ui.PrintInfo(fmt.Sprintf("Running scaffold for preset: %s", resolvedPreset.Name()))
		}

		repoName := filepath.Base(pc.ProjectPath)
		siteName := worktreeSiteName(selectedWorktree.Path, selectedWorktree.Branch, pc.DefaultBranch, pc.Config.SiteName)

		if err := pc.ScaffoldManager().RunScaffold(
			selectedWorktree.Path,
			selectedWorktree.Branch,
			repoName,
			siteName,
			resolvedPreset.Name(),
			resolvedPreset.DefaultSteps(),
			pc.Config,
			dryRun,
			verbose,
			quiet,
		); err != nil {
			ui.PrintErrorWithHint("Scaffold steps failed", err.Error())
			return err
		}

		ui.PrintDone(fmt.Sprintf("Scaffold complete: %s", selectedWorktree.Branch))
		return nil
	},
}

func selectWorktreeByContainment(worktrees []git.Worktree, cwd string) (*git.Worktree, error) {
	canonicalCWD, err := canonicalScaffoldPath(cwd)
	if err != nil {
		return nil, fmt.Errorf("canonicalizing current directory: %w", err)
	}

	var selected *git.Worktree
	selectedRoot := ""
	for i := range worktrees {
		canonicalRoot, err := canonicalScaffoldPath(worktrees[i].Path)
		if err != nil {
			continue
		}

		rel, err := filepath.Rel(canonicalRoot, canonicalCWD)
		if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}

		if selected == nil || len(canonicalRoot) > len(selectedRoot) ||
			(len(canonicalRoot) == len(selectedRoot) && canonicalRoot < selectedRoot) {
			selected = &worktrees[i]
			selectedRoot = canonicalRoot
		}
	}

	return selected, nil
}

func canonicalScaffoldPath(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	evalPath, err := filepath.EvalSymlinks(absPath)
	if err == nil {
		return evalPath, nil
	}

	return absPath, nil
}

func init() {
	rootCmd.AddCommand(scaffoldCmd)
}
