package cli

import (
	"github.com/spf13/cobra"
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
		return nil
	},
}

func init() {
	rootCmd.AddCommand(workCmd)

	workCmd.Flags().StringP("base", "b", "", "Base branch for new worktree")
	workCmd.Flags().Bool("interactive", false, "Interactive branch selection")
}
