package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// These variables are set at build time via -ldflags
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

// versionLine is the single line printed by both 'anvil version' and
// 'anvil --version'.
func versionLine() string {
	return fmt.Sprintf("anvil version %s (commit: %s, built: %s)", Version, Commit, BuildDate)
}

type versionData struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

var versionAgentCommand = agentCommand{Name: "version", Human: func(result agentResult) error {
	fmt.Println(versionLine())
	return nil
}}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long: `Display the current version of Anvil (same as 'anvil --version').

Read-only and never prompts. --json returns the version, commit, build date,
Go version and platform in an anvil.agent/v1 envelope.`,
	Args: func(cmd *cobra.Command, args []string) error { return invalidArgs(cobra.NoArgs(cmd, args)) },
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAgentCommand(cmd, versionAgentCommand, func() (agentResult, error) {
			return agentResult{Data: versionData{
				Version: Version, Commit: Commit, BuildDate: BuildDate,
				GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH,
			}}, nil
		})
	},
}

// configureVersionFlag enables 'anvil --version'. It runs at Execute time
// because main assigns Version, Commit and BuildDate after package init.
// Cobra handles the flag before any PreRun hook, so it never reaches the
// first-run wizard.
func configureVersionFlag(root *cobra.Command) {
	root.Version = versionLine()
	root.SetVersionTemplate("{{.Version}}\n")
}

func init() {
	addJSONFlag(versionCmd, versionAgentCommand)
	rootCmd.AddCommand(versionCmd)
}
