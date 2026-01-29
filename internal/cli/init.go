package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/michaeldyrynda/arbor/internal/config"
	"github.com/michaeldyrynda/arbor/internal/git"
	"github.com/michaeldyrynda/arbor/internal/presets"
	"github.com/michaeldyrynda/arbor/internal/scaffold"
	"github.com/michaeldyrynda/arbor/internal/ui"
	"github.com/michaeldyrynda/arbor/internal/utils"
)

var initCmd = &cobra.Command{
	Use:   "init [REPO] [PATH]",
	Short: "Initialise a new repository with worktree",
	Long: `Initialises a new repository as a bare git repository with an initial worktree.

Arguments:
  REPO  Repository URL (supports both full URLs and short GH format)
  PATH  Optional target directory (defaults to repository basename)`,
	Args: cobra.MaximumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		var repo string

		if len(args) > 0 {
			repo = args[0]
		} else if ui.IsInteractive() {
			input, err := ui.PromptRepoURL()
			if err != nil {
				return fmt.Errorf("prompting for repository: %w", err)
			}
			repo = input
		} else {
			return fmt.Errorf("repository URL required (run interactively or provide repo as argument)")
		}

		path := ""
		if len(args) > 1 {
			path = args[1]
		} else {
			path = utils.SanitisePath(utils.ExtractRepoName(repo))
		}

		absPath, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("getting absolute path: %w", err)
		}

		ghAvailable := isCommandAvailable("gh")

		barePath := filepath.Join(absPath, ".bare")

		var cloneErr error
		if ghAvailable {
			ui.PrintInfo("Using gh CLI for repository clone")
			cloneErr = ui.RunWithSpinner(fmt.Sprintf("Cloning %s...", repo), func() error {
				return git.CloneRepoWithGH(repo, barePath)
			})
		} else {
			cloneErr = ui.RunWithSpinner(fmt.Sprintf("Cloning %s...", repo), func() error {
				return git.CloneRepo(repo, barePath)
			})
		}
		if cloneErr != nil {
			return fmt.Errorf("cloning repository: %w", cloneErr)
		}
		ui.PrintSuccess(fmt.Sprintf("Cloned %s", repo))

		defaultBranch, err := git.GetDefaultBranch(barePath)
		if err != nil {
			defaultBranch = config.DefaultBranch
		}
		ui.PrintSuccess(fmt.Sprintf("Default branch: %s", defaultBranch))

		mainPath := filepath.Join(absPath, defaultBranch)
		ui.PrintStep(fmt.Sprintf("Creating main worktree at %s", mainPath))

		if err := git.CreateWorktree(barePath, mainPath, defaultBranch, ""); err != nil {
			return fmt.Errorf("creating main worktree: %w", err)
		}
		ui.PrintSuccess(fmt.Sprintf("Created main worktree at %s", mainPath))

		repoName := utils.SanitisePath(utils.ExtractRepoName(repo))
		siteName := utils.SanitisePath(filepath.Base(path))

		cfg := &config.Config{
			DefaultBranch: defaultBranch,
			SiteName:      siteName,
		}

		preset := mustGetString(cmd, "preset")

		presetManager := presets.NewManager()
		scaffoldManager := scaffold.NewScaffoldManager()
		presets.RegisterAllWithScaffold(scaffoldManager)

		if preset != "" {
			cfg.Preset = preset
		} else {
			detected := presetManager.Detect(mainPath)
			if detected != "" {
				cfg.Preset = detected
				ui.PrintSuccess(fmt.Sprintf("Detected: %s", detected))
			} else if ui.ShouldPrompt(cmd, true) {
				suggested := presetManager.Suggest(mainPath)
				selected, err := presets.PromptForPreset(presetManager, suggested)
				if err != nil {
					return fmt.Errorf("prompting for preset: %w", err)
				}
				cfg.Preset = selected
			}
		}

		if err := config.SaveProject(absPath, cfg); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}

		verbose := mustGetBool(cmd, "verbose")
		skipScaffold := mustGetBool(cmd, "skip-scaffold")

		if !skipScaffold && cfg.Preset != "" && verbose {
			ui.PrintInfo(fmt.Sprintf("Running scaffold for preset: %s", cfg.Preset))
		}

		if !skipScaffold {
			if err := scaffoldManager.RunScaffold(mainPath, defaultBranch, repoName, cfg.SiteName, cfg.Preset, cfg, false, verbose); err != nil {
				ui.PrintErrorWithHint("Scaffold steps failed", err.Error())
			}
		} else {
			ui.PrintInfo("Skipped scaffold (use 'arbor scaffold main' to scaffold manually)")
		}

		ui.PrintDone("Repository ready!")
		ui.PrintInfo(fmt.Sprintf("cd %s", absPath))
		ui.PrintInfo("arbor work feature/my-feature")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)

	initCmd.Flags().String("preset", "", "Project preset (laravel, php)")
	initCmd.Flags().Bool("skip-scaffold", false, "Skip scaffold steps during init")
}
