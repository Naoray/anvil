package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

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

		// Configure fetch refspec for remote tracking
		if err := git.ConfigureFetchRefspec(barePath, repo); err != nil {
			return fmt.Errorf("configuring fetch refspec: %w", err)
		}
		ui.PrintSuccess("Configured fetch refspec for remote tracking")

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

		// Check for arbor.yaml in the cloned repository
		if err := checkAndCopyRepoConfig(cmd, mainPath, absPath, cfg); err != nil {
			return err
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
		quiet := mustGetBool(cmd, "quiet")
		skipScaffold := mustGetBool(cmd, "skip-scaffold")

		if !skipScaffold && cfg.Preset != "" && verbose {
			ui.PrintInfo(fmt.Sprintf("Running scaffold for preset: %s", cfg.Preset))
		}

		if !skipScaffold {
			if err := scaffoldManager.RunScaffold(mainPath, defaultBranch, repoName, cfg.SiteName, cfg.Preset, cfg, false, verbose, quiet); err != nil {
				ui.PrintErrorWithHint("Scaffold steps failed", err.Error())
			}
		} else {
			ui.PrintInfo("Skipped scaffold (use 'arbor scaffold main' to scaffold manually)")
		}

		// Check if .arbor.local should be gitignored
		if !quiet {
			checkArborLocalGitignore(mainPath)
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
	initCmd.Flags().Bool("use-repo-config", true, "Automatically use repository config (non-interactive)")
}

// checkAndCopyRepoConfig checks for arbor.yaml in the repository and prompts to copy it
func checkAndCopyRepoConfig(cmd *cobra.Command, mainPath, projectPath string, cfg *config.Config) error {
	repoConfigPath := filepath.Join(mainPath, "arbor.yaml")
	if _, err := os.Stat(repoConfigPath); os.IsNotExist(err) {
		return nil
	}

	shouldCopy := false

	if ui.IsInteractive() {
		confirmed, err := ui.Confirm("Found arbor.yaml in repository. Copy to project root for team config?")
		if err != nil {
			return fmt.Errorf("prompting for config copy: %w", err)
		}
		shouldCopy = confirmed
	} else {
		// Non-interactive: use --use-repo-config flag (default true)
		shouldCopy = mustGetBool(cmd, "use-repo-config")
	}

	if !shouldCopy {
		return nil
	}

	projectConfigPath := filepath.Join(projectPath, "arbor.yaml")

	// Read repo config
	repoConfigData, err := os.ReadFile(repoConfigPath)
	if err != nil {
		return fmt.Errorf("reading repository config: %w", err)
	}

	// Parse and clean it (remove db_suffix if present)
	var configData map[string]interface{}
	if err := yaml.Unmarshal(repoConfigData, &configData); err != nil {
		return fmt.Errorf("parsing repository config: %w", err)
	}

	// Remove local-only fields
	delete(configData, "db_suffix")

	// Always override site_name based on local path after copying team config
	configData["site_name"] = cfg.SiteName

	// Write to project root
	cleanedData, err := yaml.Marshal(configData)
	if err != nil {
		return fmt.Errorf("marshaling cleaned config: %w", err)
	}

	if err := os.WriteFile(projectConfigPath, cleanedData, 0644); err != nil {
		return fmt.Errorf("writing project config: %w", err)
	}

	ui.PrintSuccess("Copied arbor.yaml to project root")

	// Reload config to get scaffold steps
	reloadedCfg, err := config.LoadProject(projectPath)
	if err != nil {
		return fmt.Errorf("reloading config: %w", err)
	}

	// Update cfg with reloaded scaffold/cleanup steps
	cfg.Scaffold = reloadedCfg.Scaffold
	cfg.Cleanup = reloadedCfg.Cleanup
	cfg.Preset = reloadedCfg.Preset
	cfg.Tools = reloadedCfg.Tools

	return nil
}
