package cli

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/ui"
)

var rootCmd = &cobra.Command{
	Use:   "anvil",
	Short: "Git worktree manager for agentic development",
	Long: `Anvil is a self-contained binary for managing git worktrees
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

func printBanner() {
	// Big block letters for "ANVIL" with gradient colors
	blockLetters := [][]string{
		// A
		{
			" █████╗ ",
			"██╔══██╗",
			"███████║",
			"██╔══██║",
			"██║  ██║",
			"╚═╝  ╚═╝",
		},
		// N
		{
			"███╗   ██╗",
			"████╗  ██║",
			"██╔██╗ ██║",
			"██║╚██╗██║",
			"██║ ╚████║",
			"╚═╝  ╚═══╝",
		},
		// V
		{
			"██╗   ██╗",
			"██║   ██║",
			"██║   ██║",
			"╚██╗ ██╔╝",
			" ╚████╔╝ ",
			"  ╚═══╝  ",
		},
		// I
		{
			"██╗",
			"██║",
			"██║",
			"██║",
			"██║",
			"╚═╝",
		},
		// L
		{
			"██╗     ",
			"██║     ",
			"██║     ",
			"██║     ",
			"███████╗",
			"╚══════╝",
		},
	}

	// Gradient colors - 5 colors for 5 letters
	colors := []lipgloss.Color{
		lipgloss.Color("#A5D6A7"), // Lightest green
		lipgloss.Color("#81C784"),
		lipgloss.Color("#66BB6A"),
		lipgloss.Color("#4CAF50"), // Primary green
		lipgloss.Color("#388E3C"), // Darkest green
	}

	// Render each row of the block letters
	for row := 0; row < 6; row++ {
		var lineParts []string
		for letterIdx := 0; letterIdx < len(blockLetters); letterIdx++ {
			style := lipgloss.NewStyle().
				Foreground(colors[letterIdx]).
				Bold(true)
			lineParts = append(lineParts, style.Render(blockLetters[letterIdx][row]))
		}
		fmt.Println(lipgloss.JoinHorizontal(lipgloss.Left, lineParts...))
	}

	versionStyle := lipgloss.NewStyle().
		Foreground(ui.ColorMuted).
		MarginTop(1)

	subtitleStyle := lipgloss.NewStyle().
		Foreground(ui.ColorMuted).
		MarginBottom(1)

	commandsStyle := lipgloss.NewStyle().
		Foreground(ui.Text)

	commands := `
Commands:
  link      Link an existing repository for worktree management
  unlink    Unlink a project from anvil
  work      Create or checkout a worktree
  list      List all worktrees
  cd        Navigate to a worktree
  sync      Sync current worktree with upstream branch
  remove    Remove a worktree
  prune     Remove merged worktrees
  scaffold  Run scaffold steps for a worktree
  repair    Repair git configuration for existing project
  install   Setup global configuration
  version   Show anvil version

Run 'anvil <command> --help' for more information.`

	versionLine := fmt.Sprintf("Version %s (commit: %s, built: %s)", Version, Commit, BuildDate)
	fmt.Println(versionStyle.Render(versionLine))
	fmt.Println(subtitleStyle.Render("Git Worktree Manager for Agentic Development"))
	fmt.Println(commandsStyle.Render(commands))
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
	rootCmd.PersistentFlags().Bool("quiet", false, "Suppress all output except errors")
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
