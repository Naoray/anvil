//go:build !windows

package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/ui"
)

// detectShell returns "zsh", "bash", or "fish" from $SHELL.
// Defaults to "zsh" for unknown shells.
func detectShell() string {
	shell := os.Getenv("SHELL")
	base := filepath.Base(shell)
	switch base {
	case "zsh":
		return "zsh"
	case "bash":
		return "bash"
	case "fish":
		return "fish"
	default:
		return "zsh"
	}
}

// completionInstallPath returns the target file path for a given shell.
// zsh  → $(brew --prefix)/share/zsh/site-functions/_anvil, else ~/.zsh/completions/_anvil
// bash → /etc/bash_completion.d/anvil if writable, else ~/.bash_completion.d/anvil
// fish → $XDG_CONFIG_HOME/fish/completions/anvil.fish, else ~/.config/fish/completions/anvil.fish
func completionInstallPath(shell string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}

	switch shell {
	case "zsh":
		// Determine brew prefix: env var takes priority (empty string means "no brew")
		brewPrefix, envSet := os.LookupEnv("HOMEBREW_PREFIX")
		if !envSet {
			// Not set — try running brew --prefix
			if brewOut, err := exec.Command("brew", "--prefix").Output(); err == nil {
				brewPrefix = strings.TrimSpace(string(brewOut))
			}
		}
		if brewPrefix != "" {
			brewPath := filepath.Join(brewPrefix, "share", "zsh", "site-functions", "_anvil")
			if isWritableDir(filepath.Dir(brewPath)) {
				return brewPath, nil
			}
		}
		return filepath.Join(home, ".zsh", "completions", "_anvil"), nil

	case "bash":
		systemPath := "/etc/bash_completion.d/anvil"
		if isWritableDir(filepath.Dir(systemPath)) {
			return systemPath, nil
		}
		return filepath.Join(home, ".bash_completion.d", "anvil"), nil

	case "fish":
		configBase := os.Getenv("XDG_CONFIG_HOME")
		if configBase == "" {
			configBase = filepath.Join(home, ".config")
		}
		return filepath.Join(configBase, "fish", "completions", "anvil.fish"), nil

	default:
		return "", fmt.Errorf("unsupported shell %q — supported: zsh, bash, fish", shell)
	}
}

// isWritableDir checks if the directory exists and is writable.
func isWritableDir(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	// Try to create a temp file to verify write access
	tmp, err := os.CreateTemp(dir, ".anvil-write-test-*")
	if err != nil {
		return false
	}
	name := tmp.Name()
	if err := tmp.Close(); err != nil {
		return false
	}
	if err := os.Remove(name); err != nil {
		return false
	}
	return true
}

// generateCompletionScript generates the completion script for the given shell via Cobra.
// root is the root cobra command used to generate the script.
func generateCompletionScript(root *cobra.Command, shell string) ([]byte, error) {
	var buf bytes.Buffer
	var genErr error

	switch shell {
	case "zsh":
		genErr = root.GenZshCompletion(&buf)
	case "bash":
		genErr = root.GenBashCompletionV2(&buf, true)
	case "fish":
		genErr = root.GenFishCompletion(&buf, true)
	default:
		return nil, fmt.Errorf("unsupported shell: %s", shell)
	}

	if genErr != nil {
		return nil, fmt.Errorf("generating %s completion: %w", shell, genErr)
	}

	return buf.Bytes(), nil
}

// installCompletionToPath writes the completion script to the given path, creating directories as needed.
// root is the root cobra command used to generate the script.
func installCompletionToPath(root *cobra.Command, shell, targetPath string) error {
	script, err := generateCompletionScript(root, shell)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return fmt.Errorf("creating completion directory: %w", err)
	}

	if err := os.WriteFile(targetPath, script, 0644); err != nil {
		return fmt.Errorf("writing completion file: %w", err)
	}

	return nil
}

// installCompletion generates the completion script and writes it to disk after confirming with the user.
// Returns nil if the user declines (not an error).
// root is the root cobra command; cmd is the command the wizard was invoked from (used to reach root).
func installCompletion(cmd *cobra.Command, shell string) error {
	root := cmd.Root()

	targetPath, err := completionInstallPath(shell)
	if err != nil {
		return err
	}

	ui.PrintInfo(fmt.Sprintf("Will install %s completion to: %s", shell, targetPath))

	var proceed bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Install shell completion?").
				Description(fmt.Sprintf("Write %s completion script to %s", shell, targetPath)).
				Value(&proceed),
		),
	).WithTheme(huh.ThemeCatppuccin())

	if err := form.Run(); err != nil {
		return ui.NormalizeAbort(err)
	}

	if !proceed {
		ui.PrintInfo("Skipping shell completion installation")
		return nil
	}

	if err := installCompletionToPath(root, shell, targetPath); err != nil {
		return err
	}

	ui.PrintSuccess(fmt.Sprintf("Completion installed at %s", targetPath))
	return nil
}

// overrideCompletionSubcommands replaces the RunE on each shell's completion subcommand
// to install by default, with --print to print to stdout instead.
func overrideCompletionSubcommands(rootCmd *cobra.Command) {
	rootCmd.InitDefaultCompletionCmd()

	var completionCmd *cobra.Command
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "completion" {
			completionCmd = cmd
			break
		}
	}
	if completionCmd == nil {
		return
	}

	shellNames := []string{"zsh", "bash", "fish"}

	for _, shellName := range shellNames {
		shell := shellName // capture
		for _, sub := range completionCmd.Commands() {
			if sub.Name() != shell {
				continue
			}

			original := sub.RunE
			sub.Flags().Bool("print", false, "Print completion script to stdout instead of installing")

			sub.RunE = func(cmd *cobra.Command, args []string) error {
				print, _ := cmd.Flags().GetBool("print")
				if print {
					if original != nil {
						return original(cmd, args)
					}
					return nil
				}
				return installCompletion(cmd, shell)
			}
		}
	}
}
