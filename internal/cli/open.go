package cli

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/utils"
)

const defaultEditorCmd = "cursor"

var openCmd = &cobra.Command{
	Use:   "open <WORKTREE>",
	Short: "Open a worktree in your IDE and browser",
	Long: `Opens a worktree in your configured IDE and its Herd-linked site in the browser.

Arguments:
  WORKTREE  Name of the worktree (folder name, branch name, or partial match)

Examples:
  anvil open feature-auth       # Open in IDE + browser
  anvil open auth               # Partial match
  anvil open auth --editor      # IDE only
  anvil open auth --browser     # Browser only
  anvil open auth --editor-cmd=zed  # Use Zed instead of default`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		pc, err := OpenProjectFromCWD()
		if err != nil {
			return err
		}

		query := args[0]

		worktreePath, err := findWorktreePath(pc.GitDir, query)
		if err != nil {
			return err
		}

		editorOnly := mustGetBool(cmd, "editor")
		browserOnly := mustGetBool(cmd, "browser")
		editorCmd := mustGetString(cmd, "editor-cmd")

		// Resolve editor command: flag > project config > global config > default
		if editorCmd == "" {
			editorCmd = resolveEditorCmd(pc)
		}

		url := resolveWorktreeURL(worktreePath)

		// If neither flag is set, open both
		openEditor := !browserOnly
		openBrowser := !editorOnly

		if openEditor {
			fmt.Printf("Opening %s in %s...\n", filepath.Base(worktreePath), editorCmd)
			if err := exec.Command(editorCmd, worktreePath).Start(); err != nil {
				return fmt.Errorf("opening editor: %w", err)
			}
		}

		if openBrowser {
			fmt.Printf("Opening %s in browser...\n", url)
			if err := exec.Command("open", url).Start(); err != nil {
				return fmt.Errorf("opening browser: %w", err)
			}
		}

		return nil
	},
}

// resolveWorktreeURL determines the URL for a worktree.
// Reads APP_URL from .env, falling back to https://<folder-name>.test.
func resolveWorktreeURL(worktreePath string) string {
	env := utils.ReadEnvFile(worktreePath, ".env")

	if appURL := env["APP_URL"]; appURL != "" {
		// Strip surrounding quotes if present (ReadEnvFile doesn't strip them)
		appURL = strings.Trim(appURL, `"'`)
		return appURL
	}

	folderName := filepath.Base(worktreePath)
	return "https://" + folderName + ".test"
}

// resolveEditorCmd determines the editor command from config hierarchy.
// Priority: project config > global config > default ("cursor").
func resolveEditorCmd(pc *ProjectContext) string {
	if pc.Config != nil && pc.Config.EditorCmd != "" {
		return pc.Config.EditorCmd
	}

	if pc.GlobalConfig != nil && pc.GlobalConfig.EditorCmd != "" {
		return pc.GlobalConfig.EditorCmd
	}

	return defaultEditorCmd
}

func init() {
	rootCmd.AddCommand(openCmd)

	openCmd.Flags().Bool("editor", false, "Open IDE only (skip browser)")
	openCmd.Flags().Bool("browser", false, "Open browser only (skip IDE)")
	openCmd.Flags().String("editor-cmd", "", "IDE command to use (default: cursor)")
}
