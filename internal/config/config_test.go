package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadProject_ValidConfig(t *testing.T) {
	tmpDir := t.TempDir()

	configContent := `preset: php
default_branch: main
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "anvil.yaml"), []byte(configContent), 0644))

	cfg, err := LoadProject(tmpDir)

	assert.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, "php", cfg.Preset)
	assert.Equal(t, "main", cfg.DefaultBranch)
}

func TestLoadProject_MissingConfig(t *testing.T) {
	tmpDir := t.TempDir()

	cfg, err := LoadProject(tmpDir)

	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "anvil.yaml not found")
}

func TestLoadProject_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()

	invalidContent := `preset: php
  invalid indentation that breaks yaml
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "anvil.yaml"), []byte(invalidContent), 0644))

	cfg, err := LoadProject(tmpDir)

	t.Logf("Viper behavior: invalid YAML parsed as: %+v, error: %v", cfg, err)

	assert.NoError(t, err, "viper does not return error for malformed YAML")
	assert.NotNil(t, cfg, "config is parsed even with invalid YAML")
}

func TestLoadGlobal_ValidConfig(t *testing.T) {
	tmpDir := t.TempDir()

	configContent := `default_branch: develop
detected_tools:
  php: true
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "anvil.yaml"), []byte(configContent), 0644))

	cfg, err := loadGlobalFromTestDir(tmpDir)

	assert.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, "develop", cfg.DefaultBranch)
	assert.True(t, cfg.DetectedTools["php"])
}

func TestLoadGlobal_MissingConfig(t *testing.T) {
	tmpDir := t.TempDir()

	cfg, err := loadGlobalFromTestDir(tmpDir)

	assert.Error(t, err)
	assert.Nil(t, cfg)
}

func TestGetGlobalConfigDir_XDGSet(t *testing.T) {
	xdgPath := filepath.FromSlash("/custom/config/path")
	t.Setenv("XDG_CONFIG_HOME", xdgPath)

	dir, err := GetGlobalConfigDir()

	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(xdgPath, "anvil"), dir)
}

func TestGetGlobalConfigDir_XDGNotSet(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	dir, err := GetGlobalConfigDir()

	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".config", "anvil"), dir)
}

func TestStepConfig_Unmarshal_NewFields(t *testing.T) {
	tmpDir := t.TempDir()

	configContent := `preset: php
scaffold:
  steps:
    - name: test.step
      key: DB_DATABASE
      value: "{{ .SiteName }}_{{ .DbSuffix }}"
      store_as: DatabaseName
      file: .env
      type: mysql
      args: ["--force"]
      enabled: true
      condition:
        env_file_contains:
          file: .env
          key: DB_CONNECTION
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "anvil.yaml"), []byte(configContent), 0644))

	cfg, err := LoadProject(tmpDir)

	assert.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Len(t, cfg.Scaffold.Steps, 1)

	step := cfg.Scaffold.Steps[0]
	assert.Equal(t, "test.step", step.Name)
	assert.Equal(t, "DB_DATABASE", step.Key)
	assert.Equal(t, "{{ .SiteName }}_{{ .DbSuffix }}", step.Value)
	assert.Equal(t, "DatabaseName", step.StoreAs)
	assert.Equal(t, ".env", step.File)
	assert.Equal(t, "mysql", step.Type)
	assert.Equal(t, []string{"--force"}, step.Args)
	assert.NotNil(t, step.Enabled)
	assert.True(t, *step.Enabled)
	assert.Contains(t, step.Condition, "env_file_contains")
}

func TestStepConfig_Unmarshal_OptionalFields(t *testing.T) {
	tmpDir := t.TempDir()

	configContent := `preset: php
scaffold:
  steps:
    - name: test.step
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "anvil.yaml"), []byte(configContent), 0644))

	cfg, err := LoadProject(tmpDir)

	assert.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Len(t, cfg.Scaffold.Steps, 1)

	step := cfg.Scaffold.Steps[0]
	assert.Equal(t, "test.step", step.Name)
	assert.Empty(t, step.Key)
	assert.Empty(t, step.Value)
	assert.Empty(t, step.StoreAs)
	assert.Empty(t, step.File)
	assert.Empty(t, step.Type)
	assert.Nil(t, step.Args)
	assert.Nil(t, step.Enabled)
}

func TestStepConfig_Unmarshal_EnabledFalse(t *testing.T) {
	tmpDir := t.TempDir()

	configContent := `preset: php
scaffold:
  steps:
    - name: test.step
      enabled: false
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "anvil.yaml"), []byte(configContent), 0644))

	cfg, err := LoadProject(tmpDir)

	assert.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Len(t, cfg.Scaffold.Steps, 1)

	step := cfg.Scaffold.Steps[0]
	assert.NotNil(t, step.Enabled)
	assert.False(t, *step.Enabled)
}

func loadGlobalFromTestDir(testDir string) (*GlobalConfig, error) {
	v := viper.New()

	v.SetConfigName("anvil")
	v.SetConfigType("yaml")
	v.AddConfigPath(testDir)

	if err := v.ReadInConfig(); err != nil {
		return nil, err
	}

	var config GlobalConfig
	if err := v.Unmarshal(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// Tests for linked project functionality

func TestGlobalConfig_ProjectInfo(t *testing.T) {
	tmpDir := t.TempDir()

	configContent := `default_branch: main
worktree_base: ~/.anvil/worktrees
projects:
  my-project:
    path: /home/user/projects/my-project
    default_branch: main
    preset: laravel
    site_name: my-project
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "anvil.yaml"), []byte(configContent), 0644))

	cfg, err := loadGlobalFromTestDir(tmpDir)

	assert.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, "~/.anvil/worktrees", cfg.WorktreeBase)
	assert.NotNil(t, cfg.Projects)
	assert.Contains(t, cfg.Projects, "my-project")

	project := cfg.Projects["my-project"]
	assert.Equal(t, "/home/user/projects/my-project", project.Path)
	assert.Equal(t, "main", project.DefaultBranch)
	assert.Equal(t, "laravel", project.Preset)
	assert.Equal(t, "my-project", project.SiteName)
}

func TestGlobalConfig_GetLinkedProjectByName(t *testing.T) {
	cfg := &GlobalConfig{
		Projects: map[string]*ProjectInfo{
			"my-project": {
				Path:          "/home/user/projects/my-project",
				DefaultBranch: "main",
				Preset:        "laravel",
			},
		},
	}

	project := cfg.GetLinkedProjectByName("my-project")
	assert.NotNil(t, project)
	assert.Equal(t, "/home/user/projects/my-project", project.Path)

	notFound := cfg.GetLinkedProjectByName("nonexistent")
	assert.Nil(t, notFound)
}

func TestGlobalConfig_GetLinkedProjectByName_NilProjects(t *testing.T) {
	cfg := &GlobalConfig{}

	project := cfg.GetLinkedProjectByName("my-project")
	assert.Nil(t, project)
}

func TestGlobalConfig_AddProject(t *testing.T) {
	cfg := &GlobalConfig{}

	project := &ProjectInfo{
		Path:          "/home/user/projects/test",
		DefaultBranch: "main",
		Preset:        "php",
	}

	cfg.AddProject("test", project)

	assert.NotNil(t, cfg.Projects)
	assert.Contains(t, cfg.Projects, "test")
	assert.Equal(t, project, cfg.Projects["test"])
}

func TestGlobalConfig_RemoveProject(t *testing.T) {
	cfg := &GlobalConfig{
		Projects: map[string]*ProjectInfo{
			"my-project": {Path: "/home/user/projects/my-project"},
		},
	}

	cfg.RemoveProject("my-project")
	assert.NotContains(t, cfg.Projects, "my-project")
}

func TestGlobalConfig_RemoveProject_NilProjects(t *testing.T) {
	cfg := &GlobalConfig{}
	cfg.RemoveProject("nonexistent") // Should not panic
}

func TestGlobalConfig_FindLinkedProjectFromPath(t *testing.T) {
	tmpDir := t.TempDir()
	projectPath := filepath.Join(tmpDir, "my-project")
	require.NoError(t, os.MkdirAll(projectPath, 0755))

	cfg := &GlobalConfig{
		Projects: map[string]*ProjectInfo{
			"my-project": {
				Path:          projectPath,
				DefaultBranch: "main",
			},
		},
	}

	// Exact match
	name, project := cfg.FindLinkedProjectFromPath(projectPath)
	assert.Equal(t, "my-project", name)
	assert.NotNil(t, project)

	// Subdirectory match
	subPath := filepath.Join(projectPath, "app", "Models")
	require.NoError(t, os.MkdirAll(subPath, 0755))
	name, project = cfg.FindLinkedProjectFromPath(subPath)
	assert.Equal(t, "my-project", name)
	assert.NotNil(t, project)

	// Not in any project
	name, project = cfg.FindLinkedProjectFromPath("/some/other/path")
	assert.Empty(t, name)
	assert.Nil(t, project)
}

func TestGlobalConfig_GetWorktreeBaseExpanded(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	tests := []struct {
		name         string
		worktreeBase string
		expected     string
	}{
		{
			name:         "tilde expansion",
			worktreeBase: "~/.anvil/worktrees",
			expected:     filepath.Join(home, ".anvil/worktrees"),
		},
		{
			name:         "absolute path unchanged",
			worktreeBase: "/var/anvil/worktrees",
			expected:     "/var/anvil/worktrees",
		},
		{
			name:         "empty returns empty",
			worktreeBase: "",
			expected:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &GlobalConfig{WorktreeBase: tt.worktreeBase}
			result, err := cfg.GetWorktreeBaseExpanded()
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLoadOrCreateGlobalConfig_NoExistingConfig(t *testing.T) {
	// Set XDG to a temp dir so we don't affect real config
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	cfg, err := LoadOrCreateGlobalConfig()

	assert.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, DefaultBranch, cfg.DefaultBranch)
	assert.NotNil(t, cfg.Projects)
}

func TestIsSubPath(t *testing.T) {
	tests := []struct {
		parent   string
		child    string
		expected bool
	}{
		{"/home/user/project", "/home/user/project/app", true},
		{"/home/user/project", "/home/user/project/app/Models", true},
		{"/home/user/project", "/home/user/other", false},
		{"/home/user/project", "/home/user/project", false}, // Same path is not a subpath
		{"/home/user/project", "/home/user", false},         // Parent is not a subpath of child
	}

	for _, tt := range tests {
		t.Run(tt.parent+"->"+tt.child, func(t *testing.T) {
			result := isSubPath(tt.parent, tt.child)
			assert.Equal(t, tt.expected, result)
		})
	}
}
