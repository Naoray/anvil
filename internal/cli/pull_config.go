package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/ui"
)

var pullConfigCmd = &cobra.Command{
	Use:   "pull-config",
	Short: "Copy anvil.yaml from the default branch worktree",
	Long: `Copies anvil.yaml from the default branch worktree to the project root.

Useful for propagating team configuration changes (scaffold steps,
presets, cleanup) from the main branch to the project root without
manual file copying.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		pc, err := OpenProjectFromCWD()
		if err != nil {
			return err
		}

		force := mustGetBool(cmd, "force")
		dryRun := mustGetBool(cmd, "dry-run")
		verbose := mustGetBool(cmd, "verbose")

		// Find the default branch worktree where the latest config lives
		srcWorktree := pc.GetWorktreePath(pc.DefaultBranch)
		srcConfig := filepath.Join(srcWorktree, config.ProjectConfigFile)
		dstConfig := filepath.Join(pc.ProjectPath, config.ProjectConfigFile)

		// Verify source exists
		if _, err := os.Stat(srcConfig); err != nil {
			return fmt.Errorf("no anvil.yaml found in default branch worktree (%s)", srcWorktree)
		}

		if verbose {
			ui.PrintInfo(fmt.Sprintf("Source: %s", srcConfig))
			ui.PrintInfo(fmt.Sprintf("Destination: %s", dstConfig))
		}

		// Check if destination already exists and confirm overwrite
		if _, err := os.Stat(dstConfig); err == nil && !force {
			if ui.IsInteractive() {
				confirmed, err := ui.Confirm("Overwrite existing anvil.yaml in project root?")
				if err != nil {
					return err
				}
				if !confirmed {
					ui.PrintInfo("Cancelled")
					return nil
				}
			} else {
				return fmt.Errorf("anvil.yaml already exists at project root (use --force to overwrite)")
			}
		}

		if dryRun {
			ui.PrintInfo(fmt.Sprintf("[DRY RUN] Would copy %s -> %s", srcConfig, dstConfig))
			return nil
		}

		// Copy the file
		data, err := os.ReadFile(srcConfig)
		if err != nil {
			return fmt.Errorf("reading source config: %w", err)
		}

		if err := os.WriteFile(dstConfig, data, 0644); err != nil {
			return fmt.Errorf("writing config: %w", err)
		}

		ui.PrintDone(fmt.Sprintf("Copied anvil.yaml from %s worktree to project root", pc.DefaultBranch))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(pullConfigCmd)

	pullConfigCmd.Flags().BoolP("force", "f", false, "Skip confirmation prompt")
}
