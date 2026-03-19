//go:build windows

package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func detectShell() string { return "" }

func completionInstallPath(_ string) (string, error) {
	return "", fmt.Errorf("shell completion install is not supported on Windows; use 'anvil completion <shell> --print' to get the script")
}

func installCompletionToPath(_ *cobra.Command, _, _ string) error {
	return fmt.Errorf("shell completion install is not supported on Windows")
}

func installCompletion(_ *cobra.Command, _ string) error {
	return fmt.Errorf("shell completion install is not supported on Windows; use 'anvil completion <shell> --print' to get the script")
}

func overrideCompletionSubcommands(_ *cobra.Command) {}
