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
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "arbor.yaml"), []byte(configContent), 0644))

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
	assert.Contains(t, err.Error(), "arbor.yaml not found")
}

func TestLoadProject_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()

	invalidContent := `preset: php
  invalid indentation that breaks yaml
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "arbor.yaml"), []byte(invalidContent), 0644))

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
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "arbor.yaml"), []byte(configContent), 0644))

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
	assert.Equal(t, filepath.Join(xdgPath, "arbor"), dir)
}

func TestGetGlobalConfigDir_XDGNotSet(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	dir, err := GetGlobalConfigDir()

	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".config", "arbor"), dir)
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
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "arbor.yaml"), []byte(configContent), 0644))

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
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "arbor.yaml"), []byte(configContent), 0644))

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
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "arbor.yaml"), []byte(configContent), 0644))

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

	v.SetConfigName("arbor")
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
