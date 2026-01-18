package cli

import (
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Setup global configuration",
	Long: `Sets up global configuration and detects available tools.

Creates the global arbor.yaml configuration file and detects
available tools (gh, herd, php, composer, npm).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

func init() {
	rootCmd.AddCommand(installCmd)
}
