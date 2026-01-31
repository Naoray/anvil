package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags
var Version = "dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long:  `Display the current version of Arbor.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("arbor version %s\n", Version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
