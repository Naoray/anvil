package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/ui"
)

type pullConfigDependencies struct {
	listWorktrees func(string) ([]git.Worktree, error)
	isInteractive func() bool
	confirm       func(string) (bool, error)
	printInfo     func(string)
	printDone     func(string)
}

func defaultPullConfigDependencies() pullConfigDependencies {
	return pullConfigDependencies{
		listWorktrees: git.ListWorktrees,
		isInteractive: ui.IsInteractive,
		confirm:       ui.Confirm,
		printInfo:     ui.PrintInfo,
		printDone:     ui.PrintDone,
	}
}

var pullConfigCmd = newPullConfigCommand(defaultPullConfigDependencies())

func newPullConfigCommand(deps pullConfigDependencies) *cobra.Command {
	command := &cobra.Command{
		Use:   "pull-config",
		Short: "Copy anvil.yaml from the default branch worktree",
		Long: `Copies anvil.yaml from the default branch worktree to the project root.

Useful for propagating team configuration changes (scaffold steps,
presets, cleanup) from the main branch to the project root without
manual file copying.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			pc, err := OpenProjectFromCWD()
			if err != nil {
				return err
			}

			force := mustGetBool(cmd, "force")
			dryRun := mustGetBool(cmd, "dry-run")
			verbose := mustGetBool(cmd, "verbose")

			return runPullConfigForProject(pc, force, dryRun, verbose, deps)
		},
	}

	command.Flags().BoolP("force", "f", false, "Skip confirmation prompt")
	return command
}

func resolveDefaultBranchWorktree(defaultBranch string, worktrees []git.Worktree) (git.Worktree, error) {
	candidates := make([]git.Worktree, 0, 1)
	for _, worktree := range worktrees {
		if worktree.Branch == defaultBranch && !worktree.Detached && !worktree.Bare {
			candidates = append(candidates, worktree)
		}
	}

	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if len(candidates) > 1 {
		paths := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			paths = append(paths, candidate.Path)
		}
		sort.Strings(paths)
		return git.Worktree{}, fmt.Errorf(
			"multiple registered worktrees match default branch %q: %s",
			defaultBranch,
			strings.Join(paths, ", "),
		)
	}

	detachedOnly := len(worktrees) > 0
	for _, worktree := range worktrees {
		if !worktree.Detached {
			detachedOnly = false
			break
		}
	}
	if detachedOnly {
		return git.Worktree{}, fmt.Errorf(
			"default branch %q has no attached registered worktree; all registered worktrees are detached",
			defaultBranch,
		)
	}

	return git.Worktree{}, fmt.Errorf(
		"default branch %q is absent from the registered worktree inventory",
		defaultBranch,
	)
}

func samePath(first, second string) bool {
	firstAbs, err := filepath.Abs(first)
	if err != nil {
		return false
	}
	secondAbs, err := filepath.Abs(second)
	if err != nil {
		return false
	}
	if filepath.Clean(firstAbs) == filepath.Clean(secondAbs) {
		return true
	}

	firstEval, firstErr := filepath.EvalSymlinks(firstAbs)
	secondEval, secondErr := filepath.EvalSymlinks(secondAbs)
	return firstErr == nil && secondErr == nil && firstEval == secondEval
}

func runPullConfigForProject(
	pc *ProjectContext,
	force, dryRun, verbose bool,
	deps pullConfigDependencies,
) error {
	worktrees, err := deps.listWorktrees(pc.GitDir)
	if err != nil {
		return fmt.Errorf("listing registered worktrees: %w", err)
	}

	srcWorktree, err := resolveDefaultBranchWorktree(pc.DefaultBranch, worktrees)
	if err != nil {
		return err
	}

	srcConfig := filepath.Join(srcWorktree.Path, config.ProjectConfigFile)
	dstConfig := filepath.Join(pc.ProjectPath, config.ProjectConfigFile)

	if samePath(srcWorktree.Path, pc.ProjectPath) {
		deps.printInfo(fmt.Sprintf("Source and destination are the same path (%s); nothing to do", dstConfig))
		return nil
	}

	// Verify source exists
	if _, err := os.Stat(srcConfig); err != nil {
		return fmt.Errorf("no anvil.yaml found in default branch worktree (%s)", srcWorktree.Path)
	}

	if verbose {
		deps.printInfo(fmt.Sprintf("Source: %s", srcConfig))
		deps.printInfo(fmt.Sprintf("Destination: %s", dstConfig))
	}

	// Check if destination already exists and confirm overwrite
	if _, err := os.Stat(dstConfig); err == nil && !force {
		if deps.isInteractive() {
			confirmed, err := deps.confirm("Overwrite existing anvil.yaml in project root?")
			if err != nil {
				return err
			}
			if !confirmed {
				deps.printInfo("Cancelled")
				return nil
			}
		} else {
			return fmt.Errorf("anvil.yaml already exists at project root (use --force to overwrite)")
		}
	}

	if dryRun {
		deps.printInfo(fmt.Sprintf("[DRY RUN] Would copy %s -> %s", srcConfig, dstConfig))
		return nil
	}

	// Copy the file
	data, err := os.ReadFile(srcConfig)
	if err != nil {
		return fmt.Errorf("reading source config: %w", err)
	}

	if err := os.WriteFile(dstConfig, data, 0644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	deps.printDone(fmt.Sprintf("Copied anvil.yaml from %s worktree to project root", pc.DefaultBranch))
	return nil
}

func init() {
	rootCmd.AddCommand(pullConfigCmd)
}
