package cli

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/ui"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Setup global configuration",
	Long: `Sets up global configuration and detects available tools.

Creates the global anvil.yaml configuration file and detects
available tools (gh, herd, php, composer, npm).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		title := ui.HeaderStyle.Render("Anvil Installation")

		platform := runtime.GOOS

		configDir, err := config.GetGlobalConfigDir()
		if err != nil {
			return fmt.Errorf("getting config directory: %w", err)
		}

		if err := os.MkdirAll(configDir, 0755); err != nil {
			return fmt.Errorf("creating config directory: %w", err)
		}

		detectedTools := make(map[string]bool)
		toolsInfo := make(map[string]config.ToolInfo)

		tools := []string{"gh", "herd", "php", "composer", "npm"}
		var toolRows [][]string
		for _, tool := range tools {
			path, version, err := detectTool(tool)
			if err == nil && path != "" {
				detectedTools[tool] = true
				toolsInfo[tool] = config.ToolInfo{
					Path:    path,
					Version: version,
				}
				toolRows = append(toolRows, []string{tool, "✓ found", version})
			} else {
				detectedTools[tool] = false
				toolRows = append(toolRows, []string{tool, "✗ not found", "-"})
			}
		}

		globalCfg := &config.GlobalConfig{
			DefaultBranch: config.DefaultBranch,
			DetectedTools: detectedTools,
			Tools:         toolsInfo,
			Scaffold: config.GlobalScaffoldConfig{
				ParallelDependencies: true,
				Interactive:          false,
			},
		}

		if err := config.CreateGlobalConfig(globalCfg); err != nil {
			return fmt.Errorf("saving global config: %w", err)
		}

		fmt.Println(title)
		fmt.Println()
		fmt.Printf("Platform: %s\n", platform)
		fmt.Printf("Config: %s\n", configDir)
		fmt.Println(ui.RenderStatusTable(toolRows))
		ui.PrintDone("Configuration saved")
		ui.PrintInfo("Run `anvil init <repo>` to get started")

		return nil
	},
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

func init() {
	rootCmd.AddCommand(installCmd)
}
