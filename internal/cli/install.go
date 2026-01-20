package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/michaeldyrynda/arbor/internal/config"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Setup global configuration",
	Long: `Sets up global configuration and detects available tools.

Creates the global arbor.yaml configuration file and detects
available tools (gh, herd, php, composer, npm).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		verbose := mustGetBool(cmd, "verbose")

		fmt.Printf("Arbor Global Configuration\n")
		fmt.Printf(strings.Repeat("=", 40) + "\n\n")

		platform := runtime.GOOS
		fmt.Printf("Platform: %s\n", platform)

		configDir, err := config.GetGlobalConfigDir()
		if err != nil {
			return fmt.Errorf("getting config directory: %w", err)
		}

		fmt.Printf("Config directory: %s\n", configDir)

		if err := os.MkdirAll(configDir, 0755); err != nil {
			return fmt.Errorf("creating config directory: %w", err)
		}

		detectedTools := make(map[string]bool)
		toolsInfo := make(map[string]config.ToolInfo)

		tools := []string{"gh", "herd", "php", "composer", "npm"}
		for _, tool := range tools {
			path, version, err := detectTool(tool)
			if err == nil && path != "" {
				detectedTools[tool] = true
				toolsInfo[tool] = config.ToolInfo{
					Path:    path,
					Version: version,
				}
				if verbose {
					fmt.Printf("  %s: %s (version %s)\n", tool, path, version)
				} else {
					fmt.Printf("  %s: found\n", tool)
				}
			} else {
				detectedTools[tool] = false
				if verbose {
					fmt.Printf("  %s: not found\n", tool)
				}
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

		fmt.Printf("\nGlobal configuration saved to %s\n", filepath.Join(configDir, "arbor.yaml"))
		fmt.Println("\nRun `arbor work` to create a new feature worktree.")

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
