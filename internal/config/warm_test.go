package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadProjectWarmNodeModulesDeclaration(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "anvil.yaml"), []byte(`preset: php
warm:
  node_modules:
    trust_install_scripts: true
    rebuild: "npm rebuild --Foreground-Scripts"
`), 0644))
	cfg, err := LoadProject(dir)
	require.NoError(t, err)
	require.True(t, cfg.Warm.NodeModules.TrustInstallScripts)
	require.Equal(t, "npm rebuild --Foreground-Scripts", cfg.Warm.NodeModules.Rebuild, "commands keep their case")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "anvil.yaml"), []byte("preset: php\n"), 0644))
	cfg, err = LoadProject(dir)
	require.NoError(t, err)
	require.Equal(t, WarmConfig{}, cfg.Warm, "default stays conservative")
}

func TestLoadProjectWarmValidation(t *testing.T) {
	for name, body := range map[string]string{
		"rebuild without trust": "warm:\n  node_modules:\n    rebuild: npm rebuild\n",
		"blank rebuild":         "warm:\n  node_modules:\n    trust_install_scripts: true\n    rebuild: \"  \"\n",
		"unknown key":           "warm:\n  node_modules:\n    trust_scripts: true\n",
		"unsupported tree":      "warm:\n  vendor:\n    trust_install_scripts: true\n",
		"wrong type":            "warm:\n  node_modules:\n    trust_install_scripts: maybe\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "anvil.yaml"), []byte(body), 0644))
			_, err := LoadProject(dir)
			require.Error(t, err)
			require.Contains(t, err.Error(), "warm")
		})
	}
}
