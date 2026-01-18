package cli

import (
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/michaeldyrynda/arbor/internal/config"
	"github.com/michaeldyrynda/arbor/internal/git"
	"github.com/michaeldyrynda/arbor/internal/utils"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init [REPO] [PATH]",
	Short: "Initialise a new repository with worktree",
	Long: `Initialises a new repository as a bare git repository with an initial worktree.

Arguments:
  REPO  Repository URL (supports both full URLs and short GH format)
  PATH  Optional target directory (defaults to repository basename)`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repo := args[0]

		// Determine target path
		path := ""
		if len(args) > 1 {
			path = args[1]
		} else {
			path = utils.ExtractRepoName(repo)
		}

		// Sanitise path
		path = utils.SanitisePath(path)

		// Get absolute path
		absPath, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("getting absolute path: %w", err)
		}

		// Check if gh is available
		ghAvailable := isCommandAvailable("gh")

		// Determine repo URL
		repoURL := repo
		if utils.IsGitShortFormat(repo) && ghAvailable {
			// Use gh repo clone
			fmt.Println("Using gh CLI for repository clone")
			repoURL = repo
		}

		// Create paths
		barePath := filepath.Join(absPath, ".bare")

		// Clone repository
		fmt.Printf("Cloning repository to %s\n", barePath)
		if err := git.CloneRepo(repoURL, barePath); err != nil {
			return fmt.Errorf("cloning repository: %w", err)
		}

		// Get default branch
		defaultBranch, err := git.GetDefaultBranch(barePath)
		if err != nil {
			defaultBranch = "main"
		}
		fmt.Printf("Default branch: %s\n", defaultBranch)

		// Create main worktree
		mainPath := filepath.Join(absPath, defaultBranch)
		fmt.Printf("Creating main worktree at %s\n", mainPath)

		if err := git.CreateWorktree(barePath, mainPath, defaultBranch, ""); err != nil {
			return fmt.Errorf("creating main worktree: %w", err)
		}

		// Generate project config
		cfg := &config.Config{
			DefaultBranch: defaultBranch,
		}

		preset, _ := cmd.Flags().GetString("preset")
		if preset != "" {
			cfg.Preset = preset
		}

		if err := config.SaveProject(absPath, cfg); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}

		fmt.Printf("\nArbor project initialised at %s\n", absPath)
		fmt.Println("Project config saved to arbor.yaml")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)

	initCmd.Flags().String("preset", "", "Project preset (laravel, php)")
	initCmd.Flags().Bool("interactive", false, "Interactive preset selection")
}

// isCommandAvailable checks if a command is available in PATH
func isCommandAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
