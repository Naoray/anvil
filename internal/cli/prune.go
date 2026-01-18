package cli

import (
	"github.com/spf13/cobra"
)

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Remove merged worktrees",
	Long: `Removes merged worktrees automatically.

Lists all worktrees, identifies merged ones, and provides an
interactive review before removal.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

func init() {
	rootCmd.AddCommand(pruneCmd)

	pruneCmd.Flags().BoolP("force", "f", false, "Skip interactive confirmation")
}
