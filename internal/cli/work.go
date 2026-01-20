package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/michaeldyrynda/arbor/internal/git"
	"github.com/michaeldyrynda/arbor/internal/utils"
)

var workCmd = &cobra.Command{
	Use:   "work [BRANCH] [PATH]",
	Short: "Create or checkout a feature worktree",
	Long: `Creates or checks out a new worktree for a feature branch.

Arguments:
  BRANCH  Name of the feature branch
  PATH    Optional custom path (defaults to sanitised branch name)

If no branch is provided, interactive mode allows selection from
available branches or entering a new branch name.`,
	Args: cobra.RangeArgs(0, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		pc, err := OpenProjectFromCWD()
		if err != nil {
			return err
		}

		baseBranch := mustGetString(cmd, "base")
		interactive := mustGetBool(cmd, "interactive")
		dryRun := mustGetBool(cmd, "dry-run")
		verbose := mustGetBool(cmd, "verbose")

		var branch string
		if len(args) > 0 {
			branch = args[0]
		} else if interactive {
			selected, err := selectBranchInteractive(pc.BarePath)
			if err != nil {
				return fmt.Errorf("selecting branch: %w", err)
			}
			branch = selected
		}

		if branch == "" {
			return fmt.Errorf("branch name is required (or use --interactive)")
		}

		if baseBranch == "" {
			baseBranch = pc.DefaultBranch
		}

		worktreePath := ""
		if len(args) > 1 {
			worktreePath = args[1]
		} else {
			worktreePath = filepath.Join(pc.ProjectPath, utils.SanitisePath(branch))
		}

		absWorktreePath, err := filepath.Abs(worktreePath)
		if err != nil {
			return fmt.Errorf("getting absolute path: %w", err)
		}

		exists := git.BranchExists(pc.BarePath, branch)
		if exists {
			worktrees, err := git.ListWorktrees(pc.BarePath)
			if err != nil {
				return fmt.Errorf("listing worktrees: %w", err)
			}
			for _, wt := range worktrees {
				if wt.Branch == branch {
					fmt.Printf("Worktree already exists at %s\n", wt.Path)
					return nil
				}
			}
		}

		fmt.Printf("Creating worktree for branch '%s' from '%s'\n", branch, baseBranch)
		fmt.Printf("Path: %s\n", absWorktreePath)

		if !dryRun {
			if err := git.CreateWorktree(pc.BarePath, absWorktreePath, branch, baseBranch); err != nil {
				return fmt.Errorf("creating worktree: %w", err)
			}
		} else {
			fmt.Println("[DRY RUN] Would create worktree")
		}

		if !dryRun {
			preset := pc.Config.Preset
			if preset == "" {
				preset = pc.PresetManager().Detect(absWorktreePath)
			}

			if verbose {
				fmt.Printf("Running scaffold for preset: %s\n", preset)
			}

			repoName := filepath.Base(filepath.Dir(absWorktreePath))
			if err := pc.ScaffoldManager().RunScaffold(absWorktreePath, branch, repoName, preset, pc.Config, false, verbose); err != nil {
				fmt.Printf("Warning: scaffold steps failed: %v\n", err)
			}
		} else {
			fmt.Println("[DRY RUN] Would run scaffold steps")
		}

		fmt.Printf("\nWorktree ready at %s\n", absWorktreePath)
		return nil
	},
}

func selectBranchInteractive(barePath string) (string, error) {
	localBranches, err := git.ListAllBranches(barePath)
	if err != nil {
		return "", fmt.Errorf("listing local branches: %w", err)
	}

	remoteBranches, _ := git.ListRemoteBranches(barePath)

	var allBranches []string
	allBranches = append(allBranches, "[Create new branch]")
	allBranches = append(allBranches, "")
	allBranches = append(allBranches, "Local branches:")
	for _, b := range localBranches {
		allBranches = append(allBranches, "  "+b)
	}
	if len(remoteBranches) > 0 {
		allBranches = append(allBranches, "")
		allBranches = append(allBranches, "Remote branches:")
		for _, b := range remoteBranches {
			allBranches = append(allBranches, "  "+b)
		}
	}

	fmt.Println("Available branches:")
	for i, branch := range allBranches {
		fmt.Printf("  %d. %s\n", i, branch)
	}
	fmt.Println()

	fmt.Print("Select a branch number or enter a new branch name: ")
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading input: %w", err)
	}
	input = strings.TrimSpace(input)

	if idx, err := parseBranchIndex(input, allBranches); err == nil {
		if idx == 0 {
			return promptNewBranch()
		}
		branch := allBranches[idx]
		branch = strings.TrimPrefix(branch, "  ")
		return branch, nil
	}

	return input, nil
}

func parseBranchIndex(input string, branches []string) (int, error) {
	var idx int
	_, err := fmt.Sscanf(input, "%d", &idx)
	if err != nil {
		return 0, err
	}
	if idx < 0 || idx >= len(branches) {
		return 0, fmt.Errorf("invalid index")
	}
	return idx, nil
}

func promptNewBranch() (string, error) {
	fmt.Print("Enter new branch name: ")
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading input: %w", err)
	}
	return strings.TrimSpace(input), nil
}

func isCommandAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func init() {
	rootCmd.AddCommand(workCmd)

	workCmd.Flags().StringP("base", "b", "", "Base branch for new worktree")
	workCmd.Flags().Bool("interactive", false, "Interactive branch selection")
}
