package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

const (
	// Exit codes
	ExitSuccess = iota
	ExitGeneralError
	ExitInvalidArguments
	ExitWorktreeNotFound
	ExitGitOperationFailed
	ExitConfigurationError
	ExitScaffoldStepFailed
)

const DefaultBranch = "main"

var DefaultBranchCandidates = []string{"main", "master", "develop"}

// Config represents the project configuration
type Config struct {
	SiteName      string                `mapstructure:"site_name"`
	Preset        string                `mapstructure:"preset"`
	DefaultBranch string                `mapstructure:"default_branch"`
	Scaffold      ScaffoldConfig        `mapstructure:"scaffold"`
	Cleanup       []CleanupStep         `mapstructure:"cleanup"`
	Tools         map[string]ToolConfig `mapstructure:"tools"`
}

// ScaffoldConfig represents scaffold configuration
type ScaffoldConfig struct {
	Steps    []StepConfig `mapstructure:"steps"`
	Override bool         `mapstructure:"override"`
}

// StepConfig represents a scaffold step configuration
type StepConfig struct {
	Name      string                 `mapstructure:"name"`
	Enabled   *bool                  `mapstructure:"enabled"`
	Args      []string               `mapstructure:"args"`
	Command   string                 `mapstructure:"command"`
	Condition map[string]interface{} `mapstructure:"condition"`
	From      string                 `mapstructure:"from"`
	To        string                 `mapstructure:"to"`
	Key       string                 `mapstructure:"key"`
	Value     string                 `mapstructure:"value"`
	StoreAs   string                 `mapstructure:"store_as"`
	File      string                 `mapstructure:"file"`
	Type      string                 `mapstructure:"type"`
}

// CleanupStep represents a cleanup step configuration
type CleanupStep struct {
	Name      string                 `mapstructure:"name"`
	Condition map[string]interface{} `mapstructure:"condition"`
}

// ToolConfig represents tool-specific configuration
type ToolConfig struct {
	VersionFile string `mapstructure:"version_file"`
}

// GlobalConfig represents the global configuration
type GlobalConfig struct {
	DefaultBranch string               `mapstructure:"default_branch"`
	DetectedTools map[string]bool      `mapstructure:"detected_tools"`
	Tools         map[string]ToolInfo  `mapstructure:"tools"`
	Scaffold      GlobalScaffoldConfig `mapstructure:"scaffold"`
}

// ToolInfo represents detected tool information
type ToolInfo struct {
	Path    string `mapstructure:"path"`
	Version string `mapstructure:"version"`
}

// GlobalScaffoldConfig represents global scaffold settings
type GlobalScaffoldConfig struct {
	ParallelDependencies bool `mapstructure:"parallel_dependencies"`
	Interactive          bool `mapstructure:"interactive"`
}

// LoadProject loads project configuration from arbor.yaml
func LoadProject(path string) (*Config, error) {
	v := viper.New()

	v.SetConfigName("arbor")
	v.SetConfigType("yaml")
	v.AddConfigPath(path)

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return nil, fmt.Errorf("arbor.yaml not found in %s", path)
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return &config, nil
}

// LoadGlobal loads global configuration from arbor.yaml
func LoadGlobal() (*GlobalConfig, error) {
	configDir, err := GetGlobalConfigDir()
	if err != nil {
		return nil, err
	}

	v := viper.New()

	v.SetConfigName("arbor")
	v.SetConfigType("yaml")
	v.AddConfigPath(configDir)

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return nil, fmt.Errorf("global arbor.yaml not found in %s", configDir)
		}
		return nil, fmt.Errorf("reading global config: %w", err)
	}

	var config GlobalConfig
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("parsing global config: %w", err)
	}

	return &config, nil
}

// SaveProject saves project configuration to arbor.yaml
func SaveProject(path string, config *Config) error {
	v := viper.New()

	v.SetConfigName("arbor")
	v.SetConfigType("yaml")
	v.AddConfigPath(path)

	if err := v.MergeConfigMap(map[string]interface{}{
		"site_name":      config.SiteName,
		"preset":         config.Preset,
		"default_branch": config.DefaultBranch,
	}); err != nil {
		return fmt.Errorf("merging config: %w", err)
	}

	configPath := filepath.Join(path, "arbor.yaml")
	if err := v.WriteConfigAs(configPath); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	return nil
}

// GetGlobalConfigDir returns the global config directory
func GetGlobalConfigDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "arbor"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}

	return filepath.Join(home, ".config", "arbor"), nil
}

// CreateGlobalConfig creates the global config directory and file
func CreateGlobalConfig(config *GlobalConfig) error {
	configDir, err := GetGlobalConfigDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	v := viper.New()
	v.SetConfigName("arbor")
	v.SetConfigType("yaml")
	v.AddConfigPath(configDir)

	if err := v.MergeConfigMap(map[string]interface{}{
		"default_branch": config.DefaultBranch,
		"detected_tools": config.DetectedTools,
		"scaffold":       config.Scaffold,
	}); err != nil {
		return fmt.Errorf("merging config: %w", err)
	}

	configPath := filepath.Join(configDir, "arbor.yaml")
	if err := v.WriteConfigAs(configPath); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	return nil
}

// WorktreeConfig represents worktree-local configuration
type WorktreeConfig struct {
	DbSuffix string `mapstructure:"db_suffix"`
}

// ReadWorktreeConfig reads worktree-local configuration from arbor.yaml
func ReadWorktreeConfig(worktreePath string) (*WorktreeConfig, error) {
	configPath := filepath.Join(worktreePath, "arbor.yaml")

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return &WorktreeConfig{}, nil
	}

	v := viper.New()
	v.SetConfigName("arbor")
	v.SetConfigType("yaml")
	v.AddConfigPath(worktreePath)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("reading worktree config: %w", err)
	}

	var config WorktreeConfig
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("parsing worktree config: %w", err)
	}

	return &config, nil
}

// WriteWorktreeConfig writes worktree-local configuration to arbor.yaml
func WriteWorktreeConfig(worktreePath string, data map[string]string) error {
	v := viper.New()
	v.SetConfigName("arbor")
	v.SetConfigType("yaml")
	v.AddConfigPath(worktreePath)

	dataMap := make(map[string]interface{})
	for k, v := range data {
		dataMap[k] = v
	}

	if err := v.MergeConfigMap(dataMap); err != nil {
		return fmt.Errorf("merging worktree config: %w", err)
	}

	configPath := filepath.Join(worktreePath, "arbor.yaml")

	if err := v.WriteConfigAs(configPath); err != nil {
		return fmt.Errorf("writing worktree config: %w", err)
	}

	return nil
}
