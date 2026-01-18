package cli

import (
	"github.com/spf13/cobra"
)

var removeCmd = &cobra.Command{
	Use:   "remove [BRANCH]",
	Short: "Remove a worktree with cleanup",
	Long: `Removes a worktree and runs preset-defined cleanup steps.

Arguments:
  BRANCH  Name of the branch/worktree to remove

Cleanup steps may include:
  - Removing Herd site links
  - Database cleanup prompts`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

func init() {
	rootCmd.AddCommand(removeCmd)

	removeCmd.Flags().BoolP("force", "f", false, "Skip confirmation and cleanup prompts")
}
