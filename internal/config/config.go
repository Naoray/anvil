package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
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

// Condition key constants for use in step configurations
const (
	ConditionFileExists      = "file_exists"
	ConditionCommandExists   = "command_exists"
	ConditionOS              = "os"
	ConditionEnvFileContains = "env_file_contains"
	ConditionNot             = "not"
)

// Config represents the project configuration
type Config struct {
	SiteName      string                `mapstructure:"site_name"`
	Preset        string                `mapstructure:"preset"`
	DefaultBranch string                `mapstructure:"default_branch"`
	Scaffold      ScaffoldConfig        `mapstructure:"scaffold"`
	Cleanup       CleanupConfig         `mapstructure:"cleanup"`
	Tools         map[string]ToolConfig `mapstructure:"tools"`
	Sync          SyncConfig            `mapstructure:"sync"`
}

// SyncConfig represents sync configuration for the sync command
type SyncConfig struct {
	Upstream  string `mapstructure:"upstream"`
	Strategy  string `mapstructure:"strategy"`
	Remote    string `mapstructure:"remote"`
	AutoStash *bool  `mapstructure:"auto_stash"` // Pointer to distinguish between unset and false
}

// PreFlight defines checks that run before scaffold execution.
// All checks must pass before any scaffold steps are executed.
type PreFlight struct {
	Condition map[string]interface{} `mapstructure:"condition"`
}

// ScaffoldConfig represents scaffold configuration
type ScaffoldConfig struct {
	PreFlight *PreFlight   `mapstructure:"pre_flight"`
	Steps     []StepConfig `mapstructure:"steps"`
	Override  bool         `mapstructure:"override"`
}

// StepConfig represents a scaffold step configuration
type StepConfig struct {
	Name       string                 `mapstructure:"name"`
	Enabled    *bool                  `mapstructure:"enabled"`
	Args       []string               `mapstructure:"args"`
	Command    string                 `mapstructure:"command"`
	Condition  map[string]interface{} `mapstructure:"condition"`
	From       string                 `mapstructure:"from"`
	To         string                 `mapstructure:"to"`
	Key        string                 `mapstructure:"key"`
	Keys       []string               `mapstructure:"keys"`
	Value      string                 `mapstructure:"value"`
	StoreAs    string                 `mapstructure:"store_as"`
	File       string                 `mapstructure:"file"`
	Source     string                 `mapstructure:"source"`
	SourceFile string                 `mapstructure:"source_file"`
	Type       string                 `mapstructure:"type"`
}

// GetConditionString returns a string value from the condition map for the given key.
// Returns empty string if the key doesn't exist or the value is not a string.
func (s StepConfig) GetConditionString(key string) string {
	if s.Condition == nil {
		return ""
	}
	if v, ok := s.Condition[key].(string); ok {
		return v
	}
	return ""
}

// GetConditionMap returns a map value from the condition map for the given key.
// Returns nil if the key doesn't exist or the value is not a map.
func (s StepConfig) GetConditionMap(key string) map[string]interface{} {
	if s.Condition == nil {
		return nil
	}
	if v, ok := s.Condition[key].(map[string]interface{}); ok {
		return v
	}
	return nil
}

// HasCondition checks if a condition key exists in the condition map.
func (s StepConfig) HasCondition(key string) bool {
	if s.Condition == nil {
		return false
	}
	_, exists := s.Condition[key]
	return exists
}

// CleanupStep represents a cleanup step configuration
type CleanupStep struct {
	Name      string                 `mapstructure:"name"`
	Condition map[string]interface{} `mapstructure:"condition"`
}

// GetConditionString returns a string value from the condition map for the given key.
// Returns empty string if the key doesn't exist or the value is not a string.
func (s CleanupStep) GetConditionString(key string) string {
	if s.Condition == nil {
		return ""
	}
	if v, ok := s.Condition[key].(string); ok {
		return v
	}
	return ""
}

// GetConditionMap returns a map value from the condition map for the given key.
// Returns nil if the key doesn't exist or the value is not a map.
func (s CleanupStep) GetConditionMap(key string) map[string]interface{} {
	if s.Condition == nil {
		return nil
	}
	if v, ok := s.Condition[key].(map[string]interface{}); ok {
		return v
	}
	return nil
}

// HasCondition checks if a condition key exists in the condition map.
func (s CleanupStep) HasCondition(key string) bool {
	if s.Condition == nil {
		return false
	}
	_, exists := s.Condition[key]
	return exists
}

// CleanupConfig represents cleanup configuration
type CleanupConfig struct {
	Steps []CleanupStep `mapstructure:"steps"`
}

// ToolConfig represents tool-specific configuration
type ToolConfig struct {
	VersionFile string `mapstructure:"version_file"`
}

// GlobalConfig represents the global configuration
type GlobalConfig struct {
	DefaultBranch string                  `mapstructure:"default_branch"`
	DetectedTools map[string]bool         `mapstructure:"detected_tools"`
	Tools         map[string]ToolInfo     `mapstructure:"tools"`
	Scaffold      GlobalScaffoldConfig    `mapstructure:"scaffold"`
	WorktreeBase  string                  `mapstructure:"worktree_base"`
	Projects      map[string]*ProjectInfo `mapstructure:"projects"`
}

// ProjectInfo represents a linked project's configuration
type ProjectInfo struct {
	Path          string `mapstructure:"path"`
	DefaultBranch string `mapstructure:"default_branch"`
	Preset        string `mapstructure:"preset"`
	SiteName      string `mapstructure:"site_name"`
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

// SaveProject saves project configuration to arbor.yaml.
// Preserves existing YAML structure, comments, and formatting.
func SaveProject(path string, config *Config) error {
	configPath := filepath.Join(path, "arbor.yaml")

	// Read existing file content if it exists
	var doc *yaml.Node
	var root *yaml.Node
	fileExists := false

	if content, err := os.ReadFile(configPath); err == nil {
		fileExists = true
		// Parse into yaml.Node to preserve structure
		doc = &yaml.Node{}
		if err := yaml.Unmarshal(content, doc); err != nil {
			return fmt.Errorf("parsing existing config: %w", err)
		}
		if len(doc.Content) > 0 {
			root = doc.Content[0]
		}
	}

	// If file doesn't exist or is empty, create a new document and mapping node
	if !fileExists || root == nil || root.Kind != yaml.MappingNode {
		root = &yaml.Node{
			Kind: yaml.MappingNode,
			Tag:  "!!map",
		}
		doc = &yaml.Node{
			Kind:    yaml.DocumentNode,
			Content: []*yaml.Node{root},
		}
	}

	// Helper function to set or update a value in the mapping
	setValue := func(key string, value interface{}) {
		// Find if key already exists
		for i := 0; i < len(root.Content); i += 2 {
			if root.Content[i].Value == key {
				valueNode := root.Content[i+1]
				replacement := interfaceToNode(value)
				if valueNode.Kind == yaml.ScalarNode && replacement.Kind == yaml.ScalarNode {
					valueNode.Value = replacement.Value
					valueNode.Tag = replacement.Tag
					return
				}
				// Update existing value
				root.Content[i+1] = replacement
				return
			}
		}
		// Key doesn't exist, add it
		root.Content = append(root.Content, &yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Value: key,
		})
		root.Content = append(root.Content, interfaceToNode(value))
	}

	// Helper function to set nested values (e.g., sync.upstream)
	setNestedValue := func(section string, values map[string]interface{}, orderedKeys []string) {
		// Find the section
		var sectionNode *yaml.Node
		var sectionIndex int
		for i := 0; i < len(root.Content); i += 2 {
			if root.Content[i].Value == section {
				sectionNode = root.Content[i+1]
				sectionIndex = i + 1
				break
			}
		}

		// Create section if it doesn't exist
		if sectionNode == nil {
			sectionNode = &yaml.Node{
				Kind: yaml.MappingNode,
				Tag:  "!!map",
			}
			// Add section key
			root.Content = append(root.Content, &yaml.Node{
				Kind:  yaml.ScalarNode,
				Tag:   "!!str",
				Value: section,
			})
			root.Content = append(root.Content, sectionNode)
		} else if sectionNode.Kind != yaml.MappingNode {
			sectionNode = &yaml.Node{
				Kind: yaml.MappingNode,
				Tag:  "!!map",
			}
			root.Content[sectionIndex] = sectionNode
		}

		// Update values in the section
		for _, key := range orderedKeys {
			value, ok := values[key]
			if !ok {
				continue
			}
			found := false
			for i := 0; i < len(sectionNode.Content); i += 2 {
				if sectionNode.Content[i].Value == key {
					replacement := interfaceToNode(value)
					valueNode := sectionNode.Content[i+1]
					if valueNode.Kind == yaml.ScalarNode && replacement.Kind == yaml.ScalarNode {
						valueNode.Value = replacement.Value
						valueNode.Tag = replacement.Tag
					} else {
						sectionNode.Content[i+1] = replacement
					}
					found = true
					break
				}
			}
			if !found {
				sectionNode.Content = append(sectionNode.Content, &yaml.Node{
					Kind:  yaml.ScalarNode,
					Tag:   "!!str",
					Value: key,
				})
				sectionNode.Content = append(sectionNode.Content, interfaceToNode(value))
			}
		}
	}

	// Update simple values
	if config.SiteName != "" {
		setValue("site_name", config.SiteName)
	}
	if config.Preset != "" {
		setValue("preset", config.Preset)
	}
	if config.DefaultBranch != "" {
		setValue("default_branch", config.DefaultBranch)
	}

	// Update sync config if any values are set
	if config.Sync.Upstream != "" || config.Sync.Strategy != "" || config.Sync.Remote != "" || config.Sync.AutoStash != nil {
		syncValues := make(map[string]interface{})
		if config.Sync.Upstream != "" {
			syncValues["upstream"] = config.Sync.Upstream
		}
		if config.Sync.Strategy != "" {
			syncValues["strategy"] = config.Sync.Strategy
		}
		if config.Sync.Remote != "" {
			syncValues["remote"] = config.Sync.Remote
		}
		if config.Sync.AutoStash != nil {
			syncValues["auto_stash"] = *config.Sync.AutoStash
		}
		setNestedValue("sync", syncValues, []string{"upstream", "strategy", "remote", "auto_stash"})
	}

	content, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(configPath, content, 0644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	return nil
}

// interfaceToNode converts a Go interface to a yaml.Node
func interfaceToNode(v interface{}) *yaml.Node {
	switch val := v.(type) {
	case string:
		return &yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Value: val,
		}
	case bool:
		boolStr := "false"
		if val {
			boolStr = "true"
		}
		return &yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!bool",
			Value: boolStr,
		}
	case int:
		return &yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!int",
			Value: fmt.Sprintf("%d", val),
		}
	case map[string]interface{}:
		node := &yaml.Node{
			Kind: yaml.MappingNode,
			Tag:  "!!map",
		}
		for k, v := range val {
			node.Content = append(node.Content, &yaml.Node{
				Kind:  yaml.ScalarNode,
				Tag:   "!!str",
				Value: k,
			})
			node.Content = append(node.Content, interfaceToNode(v))
		}
		return node
	case []interface{}:
		node := &yaml.Node{
			Kind: yaml.SequenceNode,
			Tag:  "!!seq",
		}
		for _, v := range val {
			node.Content = append(node.Content, interfaceToNode(v))
		}
		return node
	default:
		return &yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Value: fmt.Sprintf("%v", val),
		}
	}
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

// SaveGlobalConfig saves the global configuration to arbor.yaml
func SaveGlobalConfig(config *GlobalConfig) error {
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

	// Try to read existing config first
	_ = v.ReadInConfig()

	configMap := map[string]interface{}{
		"default_branch": config.DefaultBranch,
		"detected_tools": config.DetectedTools,
		"scaffold":       config.Scaffold,
	}

	if config.WorktreeBase != "" {
		configMap["worktree_base"] = config.WorktreeBase
	}

	if config.Projects != nil {
		configMap["projects"] = config.Projects
	}

	if err := v.MergeConfigMap(configMap); err != nil {
		return fmt.Errorf("merging config: %w", err)
	}

	configPath := filepath.Join(configDir, "arbor.yaml")
	if err := v.WriteConfigAs(configPath); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	return nil
}

// LoadOrCreateGlobalConfig loads global config or creates a new one if it doesn't exist
func LoadOrCreateGlobalConfig() (*GlobalConfig, error) {
	config, err := LoadGlobal()
	if err != nil {
		// Config doesn't exist, return empty config
		return &GlobalConfig{
			DefaultBranch: DefaultBranch,
			DetectedTools: make(map[string]bool),
			Projects:      make(map[string]*ProjectInfo),
		}, nil
	}

	// Ensure Projects map is initialized
	if config.Projects == nil {
		config.Projects = make(map[string]*ProjectInfo)
	}

	return config, nil
}

// GetLinkedProject returns the linked project info for a given path, or nil if not linked
func (gc *GlobalConfig) GetLinkedProject(path string) *ProjectInfo {
	if gc.Projects == nil {
		return nil
	}

	// Normalize the path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil
	}

	// Check if the exact path matches a project
	for _, project := range gc.Projects {
		projectAbs, err := filepath.Abs(project.Path)
		if err != nil {
			continue
		}
		if projectAbs == absPath {
			return project
		}
	}

	return nil
}

// GetLinkedProjectByName returns the linked project info by name
func (gc *GlobalConfig) GetLinkedProjectByName(name string) *ProjectInfo {
	if gc.Projects == nil {
		return nil
	}
	return gc.Projects[name]
}

// FindLinkedProjectFromPath checks if a path is inside a linked project
func (gc *GlobalConfig) FindLinkedProjectFromPath(path string) (string, *ProjectInfo) {
	if gc.Projects == nil {
		return "", nil
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", nil
	}

	for name, project := range gc.Projects {
		projectAbs, err := filepath.Abs(project.Path)
		if err != nil {
			continue
		}

		// Check if path is the project root or inside it
		if absPath == projectAbs || isSubPath(projectAbs, absPath) {
			return name, project
		}
	}

	return "", nil
}

// isSubPath checks if child is a subdirectory of parent
func isSubPath(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	// If the relative path doesn't start with "..", it's a subpath
	return len(rel) > 0 && rel[0] != '.'
}

// AddProject adds a project to the global config
func (gc *GlobalConfig) AddProject(name string, project *ProjectInfo) {
	if gc.Projects == nil {
		gc.Projects = make(map[string]*ProjectInfo)
	}
	gc.Projects[name] = project
}

// RemoveProject removes a project from the global config
func (gc *GlobalConfig) RemoveProject(name string) {
	if gc.Projects != nil {
		delete(gc.Projects, name)
	}
}

// GetWorktreeBaseExpanded returns the worktree base path with ~ expanded
func (gc *GlobalConfig) GetWorktreeBaseExpanded() (string, error) {
	if gc.WorktreeBase == "" {
		return "", nil
	}

	base := gc.WorktreeBase
	if len(base) > 0 && base[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expanding home directory: %w", err)
		}
		base = filepath.Join(home, base[1:])
	}

	return base, nil
}
