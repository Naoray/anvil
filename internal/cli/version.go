package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// These variables are set at build time via -ldflags
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long:  `Display the current version of Arbor.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("arbor version %s (commit: %s, built: %s)\n", Version, Commit, BuildDate)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
