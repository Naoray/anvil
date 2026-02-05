package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/michaeldyrynda/arbor/internal/config"
	"github.com/michaeldyrynda/arbor/internal/git"
	"github.com/michaeldyrynda/arbor/internal/presets"
	"github.com/michaeldyrynda/arbor/internal/ui"
)

var linkCmd = &cobra.Command{
	Use:   "link [PATH]",
	Short: "Link an existing git repository for centralized worktree management",
	Long: `Links an existing git repository to arbor for centralized worktree management.

This allows you to keep your project folder clean while storing worktrees
in a centralized location (configured via worktree_base in global config).

Arguments:
  PATH  Optional path to the git repository (defaults to current directory)

Examples:
  # Link current directory
  arbor link

  # Link with a specific preset
  arbor link --preset laravel

  # Link a specific path
  arbor link ~/Projects/my-app

  # Link with a custom name
  arbor link --name my-custom-name`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Determine the path to link
		path := "."
		if len(args) > 0 {
			path = args[0]
		}

		absPath, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("getting absolute path: %w", err)
		}

		// Validate it's a git repository
		if !git.IsGitRepo(absPath) {
			return fmt.Errorf("%s is not a git repository (no .git directory found)", absPath)
		}

		// Check if it's already an arbor project (has .bare)
		if git.IsArborProject(absPath) {
			return fmt.Errorf("%s is already an arbor project (has .bare directory). Use 'arbor work' directly", absPath)
		}

		// Load or create global config
		globalCfg, err := config.LoadOrCreateGlobalConfig()
		if err != nil {
			return fmt.Errorf("loading global config: %w", err)
		}

		// Check if worktree_base is configured
		worktreeBase, err := globalCfg.GetWorktreeBaseExpanded()
		if err != nil {
			return fmt.Errorf("expanding worktree base: %w", err)
		}

		if worktreeBase == "" {
			ui.PrintWarning("No worktree_base configured in global config")
			ui.PrintInfo("Worktrees will be created in ~/.arbor/worktrees by default")
			ui.PrintInfo("To change this, run: arbor config set worktree_base <path>")

			// Set default worktree base
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("getting home directory: %w", err)
			}
			worktreeBase = filepath.Join(home, ".arbor", "worktrees")
			globalCfg.WorktreeBase = "~/.arbor/worktrees"
		}

		// Determine project name
		name := mustGetString(cmd, "name")
		if name == "" {
			name = filepath.Base(absPath)
		}

		// Check if project is already linked
		if existing := globalCfg.GetLinkedProjectByName(name); existing != nil {
			if existing.Path == absPath {
				ui.PrintWarning(fmt.Sprintf("Project '%s' is already linked", name))
				return nil
			}
			return fmt.Errorf("a project named '%s' already exists at %s. Use --name to specify a different name", name, existing.Path)
		}

		// Get the git directory and detect default branch
		gitDir, _, err := git.FindGitDir(absPath)
		if err != nil {
			return fmt.Errorf("finding git directory: %w", err)
		}

		defaultBranch, err := git.GetDefaultBranch(gitDir)
		if err != nil {
			defaultBranch = config.DefaultBranch
		}

		// Detect or prompt for preset
		preset := mustGetString(cmd, "preset")
		if preset == "" {
			presetManager := presets.NewManager()
			detected := presetManager.Detect(absPath)
			if detected != "" {
				preset = detected
				ui.PrintSuccess(fmt.Sprintf("Detected preset: %s", detected))
			} else if ui.ShouldPrompt(cmd, true) {
				suggested := presetManager.Suggest(absPath)
				selected, err := presets.PromptForPreset(presetManager, suggested)
				if err != nil {
					return fmt.Errorf("prompting for preset: %w", err)
				}
				preset = selected
			}
		}

		// Determine site name
		siteName := mustGetString(cmd, "site-name")
		if siteName == "" {
			siteName = name
		}

		// Create project info
		projectInfo := &config.ProjectInfo{
			Path:          absPath,
			DefaultBranch: defaultBranch,
			Preset:        preset,
			SiteName:      siteName,
		}

		// Add to global config
		globalCfg.AddProject(name, projectInfo)

		// Save global config
		if err := config.SaveGlobalConfig(globalCfg); err != nil {
			return fmt.Errorf("saving global config: %w", err)
		}

		// Create worktree directory for this project
		projectWorktreeDir := filepath.Join(worktreeBase, name)
		if err := os.MkdirAll(projectWorktreeDir, 0755); err != nil {
			return fmt.Errorf("creating worktree directory: %w", err)
		}

		ui.PrintSuccess(fmt.Sprintf("Linked '%s' from %s", name, absPath))
		ui.PrintInfo(fmt.Sprintf("Default branch: %s", defaultBranch))
		if preset != "" {
			ui.PrintInfo(fmt.Sprintf("Preset: %s", preset))
		}
		ui.PrintInfo(fmt.Sprintf("Worktrees will be stored in: %s", projectWorktreeDir))
		ui.PrintInfo("")
		ui.PrintInfo("Create a new worktree with:")
		ui.PrintInfo(fmt.Sprintf("  cd %s && arbor work feature/my-feature", absPath))

		return nil
	},
}

func init() {
	rootCmd.AddCommand(linkCmd)

	linkCmd.Flags().String("preset", "", "Project preset (laravel, php)")
	linkCmd.Flags().String("name", "", "Custom name for the linked project (defaults to directory name)")
	linkCmd.Flags().String("site-name", "", "Site name for scaffold steps (defaults to project name)")
}
