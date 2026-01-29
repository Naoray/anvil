package cli

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/michaeldyrynda/arbor/internal/ui"
)

var rootCmd = &cobra.Command{
	Use:   "arbor",
	Short: "Git worktree manager for agentic development",
	Long: `Arbor is a self-contained binary for managing git worktrees
to assist with agentic development of applications.
It is cross-project, cross-language, and cross-environment compatible.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if noColor || !ui.IsInteractive() {
			return cmd.Help()
		}
		printBanner()
		return nil
	},
}

var noColor bool

const banner = `
   _____         __
  /  _  \  _____\_ |__   ___________
 /  /_\  \ \__  \| __ \ /  _ \_  __ \
/    |    \ / __ \| \_\ (  <_> )  | \/
\____|__  /(____  /___  /\____/|__|
        \/      \/    \/

Git Worktree Manager for Agentic Development

Commands:
  init      Initialize a new repository
  work      Create or checkout a worktree
  list      List all worktrees
  remove    Remove a worktree
  prune     Remove merged worktrees
  scaffold  Run scaffold steps for a worktree
  destroy   Completely destroy an arbor project
  install   Setup global configuration

Run 'arbor <command> --help' for more information.`

func printBanner() {
	style := lipgloss.NewStyle().
		Foreground(ui.Primary).
		Bold(true)
	fmt.Println(style.Render(banner))
}

func Execute() error {
	rootCmd.SilenceUsage = true
	if err := rootCmd.Execute(); err != nil {
		if ui.IsAbort(err) {
			return nil
		}
		return err
	}
	return nil
}

func init() {
	rootCmd.PersistentFlags().Bool("dry-run", false, "Preview operations without executing")
	rootCmd.PersistentFlags().Bool("verbose", false, "Enable verbose output")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable colored output")
	rootCmd.PersistentFlags().Bool("no-interactive", false, "Disable interactive prompts")
}

func mustGetString(cmd *cobra.Command, name string) string {
	value, err := cmd.Flags().GetString(name)
	if err != nil {
		panic(fmt.Sprintf("programming error: flag %q not defined: %v", name, err))
	}
	return value
}

func mustGetBool(cmd *cobra.Command, name string) bool {
	value, err := cmd.Flags().GetBool(name)
	if err != nil {
		panic(fmt.Sprintf("programming error: flag %q not defined: %v", name, err))
	}
	return value
}
