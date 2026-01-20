package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/michaeldyrynda/arbor/internal/config"
	"github.com/michaeldyrynda/arbor/internal/git"
	"github.com/michaeldyrynda/arbor/internal/presets"
	"github.com/michaeldyrynda/arbor/internal/scaffold"
	"github.com/spf13/cobra"
)

var removeCmd = &cobra.Command{
	Use:   "remove [FOLDER]",
	Short: "Remove a worktree with cleanup",
	Long: `Removes a worktree and runs preset-defined cleanup steps.

Arguments:
  FOLDER  Name of the worktree folder to remove (e.g., feature-test-change)

Cleanup steps may include:
  - Removing Herd site links
  - Database cleanup prompts`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		folderName := args[0]

		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting current directory: %w", err)
		}

		barePath, err := git.FindBarePath(cwd)
		if err != nil {
			return fmt.Errorf("finding bare repository: %w", err)
		}

		projectPath := filepath.Dir(barePath)
		cfg, err := config.LoadProject(projectPath)
		if err != nil {
			return fmt.Errorf("loading project config: %w", err)
		}

		force, _ := cmd.Flags().GetBool("force")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		verbose, _ := cmd.Flags().GetBool("verbose")

		worktrees, err := git.ListWorktrees(barePath)
		if err != nil {
			return fmt.Errorf("listing worktrees: %w", err)
		}

		var targetWorktree *git.Worktree
		for _, wt := range worktrees {
			if filepath.Base(wt.Path) == folderName {
				targetWorktree = &wt
				break
			}
		}

		if targetWorktree == nil {
			return fmt.Errorf("worktree '%s' not found", folderName)
		}

		if !force {
			fmt.Printf("Remove worktree '%s' at %s?\n", targetWorktree.Branch, targetWorktree.Path)
			fmt.Print("This will run cleanup steps. Continue? [y/N]: ")

			reader := bufio.NewReader(os.Stdin)
			response, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("reading confirmation: %w", err)
			}

			response = strings.TrimSpace(response)
			if response != "y" && response != "Y" {
				fmt.Println("Cancelled.")
				return nil
			}
		}

		fmt.Printf("Removing worktree: %s (%s)\n", targetWorktree.Branch, targetWorktree.Path)

		if !dryRun {
			presetManager := presets.NewManager()
			scaffoldManager := scaffold.NewScaffoldManager()
			presets.RegisterAllWithScaffold(scaffoldManager)

			preset := cfg.Preset
			if preset == "" {
				preset = presetManager.Detect(targetWorktree.Path)
			}

			fmt.Printf("Running cleanup steps for preset: %s\n", preset)

			if err := scaffoldManager.RunCleanup(targetWorktree.Path, targetWorktree.Branch, "", preset, cfg, false, verbose); err != nil {
				fmt.Printf("Warning: cleanup steps failed: %v\n", err)
			}

			if err := git.RemoveWorktree(targetWorktree.Path, true); err != nil {
				return fmt.Errorf("removing worktree: %w", err)
			}

			parentDir := filepath.Dir(targetWorktree.Path)
			entries, err := os.ReadDir(parentDir)
			if err == nil && len(entries) == 0 {
				if err := os.Remove(parentDir); err != nil {
					fmt.Printf("Warning: could not remove empty directory %s: %v\n", parentDir, err)
				}
			}
		} else {
			fmt.Println("[DRY RUN] Would run cleanup steps and remove worktree")
		}

		fmt.Println("Worktree removed successfully.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(removeCmd)

	removeCmd.Flags().BoolP("force", "f", false, "Skip confirmation and cleanup prompts")
}
