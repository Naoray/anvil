package cli

import (
	"fmt"
	"path/filepath"

	"github.com/michaeldyrynda/arbor/internal/config"
	"github.com/michaeldyrynda/arbor/internal/git"
	"github.com/michaeldyrynda/arbor/internal/presets"
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

		path := ""
		if len(args) > 1 {
			path = args[1]
		} else {
			path = utils.ExtractRepoName(repo)
		}

		path = utils.SanitisePath(path)

		absPath, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("getting absolute path: %w", err)
		}

		ghAvailable := isCommandAvailable("gh")

		barePath := filepath.Join(absPath, ".bare")

		fmt.Printf("Cloning repository to %s\n", barePath)

		var cloneErr error
		if ghAvailable {
			fmt.Println("Using gh CLI for repository clone")
			cloneErr = git.CloneRepoWithGH(repo, barePath)
		} else {
			cloneErr = git.CloneRepo(repo, barePath)
		}
		if cloneErr != nil {
			return fmt.Errorf("cloning repository: %w", cloneErr)
		}

		defaultBranch, err := git.GetDefaultBranch(barePath)
		if err != nil {
			defaultBranch = "main"
		}
		fmt.Printf("Default branch: %s\n", defaultBranch)

		mainPath := filepath.Join(absPath, defaultBranch)
		fmt.Printf("Creating main worktree at %s\n", mainPath)

		if err := git.CreateWorktree(barePath, mainPath, defaultBranch, ""); err != nil {
			return fmt.Errorf("creating main worktree: %w", err)
		}

		cfg := &config.Config{
			DefaultBranch: defaultBranch,
		}

		preset, _ := cmd.Flags().GetString("preset")
		interactive, _ := cmd.Flags().GetBool("interactive")

		if preset != "" {
			cfg.Preset = preset
		} else if interactive {
			suggested := presetManager.Suggest(mainPath)
			selected, err := presets.PromptForPreset(presetManager, suggested)
			if err != nil {
				return fmt.Errorf("prompting for preset: %w", err)
			}
			cfg.Preset = selected
		} else {
			detected := presetManager.Detect(mainPath)
			if detected != "" {
				cfg.Preset = detected
				fmt.Printf("Detected preset: %s\n", detected)
			}
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
