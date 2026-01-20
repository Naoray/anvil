package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	arborerrors "github.com/michaeldyrynda/arbor/internal/errors"
	"github.com/michaeldyrynda/arbor/internal/git"
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

		pc, err := OpenProjectFromCWD()
		if err != nil {
			return err
		}

		force := mustGetBool(cmd, "force")
		dryRun := mustGetBool(cmd, "dry-run")
		verbose := mustGetBool(cmd, "verbose")

		worktrees, err := git.ListWorktrees(pc.BarePath)
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
			return fmt.Errorf("worktree '%s' not found: %w", folderName, arborerrors.ErrWorktreeNotFound)
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
			preset := pc.Config.Preset
			if preset == "" {
				preset = pc.PresetManager().Detect(targetWorktree.Path)
			}

			fmt.Printf("Running cleanup steps for preset: %s\n", preset)

			if err := pc.ScaffoldManager().RunCleanup(targetWorktree.Path, targetWorktree.Branch, "", preset, pc.Config, false, verbose); err != nil {
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
