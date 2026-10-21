package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGlobalChecksSlotsLoadAndSurviveSave(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	configDir := filepath.Join(tmpDir, "anvil")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	content := "default_branch: main\nchecks:\n  slots: 2\n  max_load: 6.5\n  load_wait: 2m\n"
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ProjectConfigFile), []byte(content), 0o644))

	cfg, err := LoadGlobal()
	require.NoError(t, err)
	require.Equal(t, GlobalChecksConfig{Slots: 2, MaxLoad: 6.5, LoadWait: "2m"}, cfg.Checks)

	require.NoError(t, SaveGlobalConfig(cfg))
	reloaded, err := LoadGlobal()
	require.NoError(t, err)
	require.Equal(t, cfg.Checks, reloaded.Checks, "saving the global config keeps check slot settings")
}

func TestCheckSlotsDeclaration(t *testing.T) {
	dir := t.TempDir()
	write := func(content string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "anvil.yaml"), []byte(content), 0o644))
	}
	write("checks:\n  bench:\n    slots: 2\n    commands: [\"true\"]\n  plain:\n    commands: [\"true\"]\n")
	cfg, err := LoadProject(dir)
	require.NoError(t, err)
	require.Equal(t, 2, cfg.Checks[0].Slots)
	require.Zero(t, cfg.Checks[1].Slots)
	encoded, err := json.Marshal(cfg.Checks[1])
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "slots", "an undeclared weight leaves existing definition digests unchanged")

	write("checks:\n  bench:\n    slots: -1\n    commands: [\"true\"]\n")
	_, err = LoadProject(dir)
	require.ErrorContains(t, err, "slots must be at least 1")
}
