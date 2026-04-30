package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	anvilagent "github.com/naoray/anvil/anvil-agent"
	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/ui"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Setup global configuration and run setup wizard",
	Long: `Runs the interactive setup wizard to configure anvil.

The wizard checks your PATH, detects Herd/Valet, installs shell completions,
sets a default projects root, and optionally installs AI CLI skills.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInstallWizard(cmd)
	},
}

// runInstallWizard runs the full 5-step interactive setup wizard.
func runInstallWizard(cmd *cobra.Command) error {
	globalCfg, err := config.LoadOrCreateGlobalConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	fmt.Println(ui.HeaderStyle.Render("Anvil Setup Wizard"))
	fmt.Println()

	// Step 1: PATH check
	fmt.Println(ui.HeaderStyle.Render("[1/5] PATH check"))
	if path, err := exec.LookPath("anvil"); err == nil {
		ui.PrintSuccess(fmt.Sprintf("anvil found at %s", path))
	} else {
		hint := pathExportHint()
		ui.PrintWarning("anvil not found in PATH")
		ui.PrintInfo(hint)
	}
	fmt.Println()

	// Step 2: Herd / Valet check
	fmt.Println(ui.HeaderStyle.Render("[2/5] Herd / Valet check"))
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}
	herdPaths := []struct{ path, label string }{
		{filepath.Join(home, "Library", "Application Support", "Herd"), "~/Library/Application Support/Herd"},
		{filepath.Join(home, ".config", "herd"), "~/.config/herd"},
	}
	valetPath := filepath.Join(home, ".valet")

	herdFound := false
	for _, h := range herdPaths {
		if _, err := os.Stat(h.path); err == nil {
			ui.PrintSuccess(fmt.Sprintf("Herd detected at %s", h.label))
			herdFound = true
			break
		}
	}
	if !herdFound {
		if _, err := os.Stat(valetPath); err == nil {
			ui.PrintSuccess("Valet detected at ~/.valet")
		} else {
			ui.PrintWarning("Herd or Valet not detected — worktree features require one of these")
		}
	}
	fmt.Println()

	// Step 3: Shell completion
	fmt.Println(ui.HeaderStyle.Render("[3/5] Shell completion"))
	shell := detectShell()
	if err := installCompletion(cmd, shell); err != nil {
		// Non-fatal: warn and continue
		ui.PrintWarning(fmt.Sprintf("Could not install completion: %v", err))
	}
	fmt.Println()

	// Step 4: Default projects root
	fmt.Println(ui.HeaderStyle.Render("[4/5] Default projects root"))
	if globalCfg.DefaultProjectsRoot == "" {
		var projectsRoot string
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Default projects root").
					Description("Directory where your projects live (used as a hint for anvil link)").
					Placeholder("~/Code").
					Value(&projectsRoot),
			),
		).WithTheme(huh.ThemeCatppuccin())

		if err := form.Run(); err != nil {
			if !ui.IsAbort(err) {
				return fmt.Errorf("prompting for projects root: %w", err)
			}
		} else if projectsRoot != "" {
			globalCfg.DefaultProjectsRoot = projectsRoot
		}
	} else {
		ui.PrintSuccess(fmt.Sprintf("Default projects root already set: %s", globalCfg.DefaultProjectsRoot))
	}
	fmt.Println()

	// Step 5: AI skill setup
	fmt.Println(ui.HeaderStyle.Render("[5/5] AI CLI skill setup"))
	if err := runAISkillSetup(cmd); err != nil {
		ui.PrintWarning(fmt.Sprintf("Skill setup skipped: %v", err))
	}
	fmt.Println()

	// Detect tools and save
	detectedTools := make(map[string]bool)
	toolsInfo := make(map[string]config.ToolInfo)
	tools := []string{"gh", "herd", "php", "composer", "npm"}
	var toolRows [][]string
	for _, tool := range tools {
		toolPath, version, err := detectTool(tool)
		if err == nil && toolPath != "" {
			detectedTools[tool] = true
			toolsInfo[tool] = config.ToolInfo{Path: toolPath, Version: version}
			toolRows = append(toolRows, []string{tool, "✓ found", version})
		} else {
			detectedTools[tool] = false
			toolRows = append(toolRows, []string{tool, "✗ not found", "-"})
		}
	}

	globalCfg.DetectedTools = detectedTools
	globalCfg.Tools = toolsInfo
	if globalCfg.DefaultBranch == "" {
		globalCfg.DefaultBranch = config.DefaultBranch
	}
	if globalCfg.Scaffold == (config.GlobalScaffoldConfig{}) {
		globalCfg.Scaffold = config.GlobalScaffoldConfig{
			ParallelDependencies: true,
			Interactive:          false,
		}
	}
	globalCfg.SetupComplete = true

	if err := config.SaveGlobalConfig(globalCfg); err != nil {
		return fmt.Errorf("saving global config: %w", err)
	}

	configDir, _ := config.GetGlobalConfigDir()
	fmt.Printf("Platform: %s\n", runtime.GOOS)
	fmt.Printf("Config: %s\n", configDir)
	fmt.Println(ui.RenderStatusTable(toolRows))

	ui.PrintDone("Anvil is ready. Run 'anvil link <repo>' to get started.")
	return nil
}

// pathExportHint returns an OS-appropriate hint for adding anvil to PATH.
func pathExportHint() string {
	switch runtime.GOOS {
	case "darwin":
		return `Add to ~/.zshrc: export PATH="$HOME/.local/bin:$PATH"`
	case "linux":
		return `Add to ~/.bashrc: export PATH="$HOME/.local/bin:$PATH"`
	default:
		return "Add the anvil binary directory to your PATH"
	}
}

// runAISkillSetup detects AI CLIs and optionally installs skills.
func runAISkillSetup(_ *cobra.Command) error {
	type aiTool struct {
		name string
		// check returns true if the tool is available
		check func() bool
		// dest returns the skill destination path
		dest func() (string, error)
		// content is the skill file content
		content []byte
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}

	available := []aiTool{
		{
			name: "Claude Code (claude)",
			check: func() bool {
				_, err := exec.LookPath("claude")
				return err == nil
			},
			dest: func() (string, error) {
				return filepath.Join(home, ".claude", "skills", "anvil-agent", "SKILL.md"), nil
			},
			content: anvilagent.Content,
		},
		{
			name: "Codex CLI (codex)",
			check: func() bool {
				_, err := exec.LookPath("codex")
				return err == nil
			},
			dest: func() (string, error) {
				return filepath.Join(home, ".codex", "skills", "anvil-agent", "SKILL.md"), nil
			},
			content: anvilagent.Content,
		},
	}

	var detected []aiTool
	for _, tool := range available {
		if tool.check() {
			detected = append(detected, tool)
		}
	}

	// Also check gh copilot
	copilotAvailable := false
	if out, err := exec.Command("gh", "copilot", "--version").Output(); err == nil && len(out) > 0 {
		copilotAvailable = true
	}

	if len(detected) == 0 && !copilotAvailable {
		ui.PrintInfo("No AI CLIs detected, skipping")
		return nil
	}

	if copilotAvailable {
		ui.PrintInfo("GitHub Copilot CLI detected — no skill file needed (uses AGENTS.md automatically)")
	}

	if len(detected) == 0 {
		return nil
	}

	// Build multiselect options from detected tools
	options := make([]huh.Option[string], len(detected))
	for i, tool := range detected {
		options[i] = huh.NewOption(tool.name, tool.name)
	}

	var selected []string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Install AI CLI skills").
				Description("Space to toggle, Enter to confirm").
				Options(options...).
				Value(&selected),
		),
	).WithTheme(huh.ThemeCatppuccin())

	if err := form.Run(); err != nil {
		return ui.NormalizeAbort(err)
	}

	selectedSet := make(map[string]bool)
	for _, s := range selected {
		selectedSet[s] = true
	}

	for _, tool := range detected {
		if !selectedSet[tool.name] {
			continue
		}

		destPath, err := tool.dest()
		if err != nil {
			ui.PrintWarning(fmt.Sprintf("Could not determine skill path for %s: %v", tool.name, err))
			continue
		}

		// Check if file already exists
		if _, err := os.Stat(destPath); err == nil {
			if diffOut := skillDiff(destPath, tool.content); diffOut != "" {
				fmt.Println(diffOut)
			} else {
				ui.PrintInfo(fmt.Sprintf("Skill at %s is already up to date", destPath))
				continue
			}

			var overwrite bool
			confirmForm := huh.NewForm(
				huh.NewGroup(
					huh.NewConfirm().
						Title("Skill already installed — overwrite?").
						Description(fmt.Sprintf("Diff shown above. Overwrite %s?", destPath)).
						Value(&overwrite),
				),
			).WithTheme(huh.ThemeCatppuccin())

			if err := confirmForm.Run(); err != nil || !overwrite {
				ui.PrintInfo(fmt.Sprintf("Skipping %s skill update", tool.name))
				continue
			}
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			ui.PrintWarning(fmt.Sprintf("Could not create skill directory for %s: %v", tool.name, err))
			continue
		}

		if err := os.WriteFile(destPath, tool.content, 0644); err != nil {
			ui.PrintWarning(fmt.Sprintf("Could not write skill for %s: %v", tool.name, err))
			continue
		}

		ui.PrintSuccess(fmt.Sprintf("Skill installed at %s", destPath))
	}

	return nil
}

func detectTool(name string) (string, string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", "", fmt.Errorf("not found")
	}

	version, err := getToolVersion(name, path)
	if err != nil {
		version = "unknown"
	}

	return path, version, nil
}

func getToolVersion(name, path string) (string, error) {
	var cmd *exec.Cmd

	switch name {
	case "gh":
		cmd = exec.Command(path, "version")
	case "php":
		cmd = exec.Command(path, "-v")
	case "composer":
		cmd = exec.Command(path, "--version")
	case "npm":
		cmd = exec.Command(path, "--version")
	case "herd":
		cmd = exec.Command(path, "version")
	default:
		return "", fmt.Errorf("unknown tool")
	}

	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return extractVersion(string(output), name), nil
}

func extractVersion(output, tool string) string {
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	switch tool {
	case "gh":
		for _, line := range lines {
			if strings.Contains(line, "gh version") {
				parts := strings.Split(line, " ")
				if len(parts) >= 3 {
					return strings.TrimPrefix(parts[2], "v")
				}
			}
		}
	case "php":
		for _, line := range lines {
			if strings.Contains(line, "PHP") {
				parts := strings.Split(line, " ")
				if len(parts) >= 2 {
					return strings.TrimPrefix(parts[1], "v")
				}
			}
		}
	case "composer":
		for _, line := range lines {
			if strings.Contains(line, "Composer version") {
				parts := strings.Split(line, " ")
				if len(parts) >= 3 {
					return strings.TrimPrefix(parts[2], "v")
				}
			}
		}
	case "npm":
		for _, line := range lines {
			if strings.Contains(line, ".") {
				return strings.TrimSpace(line)
			}
		}
	case "herd":
		for _, line := range lines {
			if strings.Contains(line, "version") || strings.Contains(line, "Herd") {
				parts := strings.Fields(line)
				for _, part := range parts {
					if strings.HasPrefix(part, "v") && len(part) > 1 {
						return strings.TrimPrefix(part, "v")
					}
				}
			}
		}
	}

	return ""
}

// skillDiff returns a unified diff between the on-disk skill file and the new
// content. Returns an empty string when the files are identical or when diff
// is unavailable, so callers can treat "" as "no change needed".
func skillDiff(existingPath string, newContent []byte) string {
	tmp, err := os.CreateTemp("", "anvil-skill-new-*")
	if err != nil {
		return ""
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write(newContent); err != nil {
		return ""
	}
	if err := tmp.Close(); err != nil {
		return ""
	}

	out, _ := exec.Command("diff", "-u", existingPath, tmp.Name()).Output()
	return strings.TrimRight(string(out), "\n")
}

func init() {
	rootCmd.AddCommand(installCmd)
}
