package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadProjectPruneProtectedPatterns(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "anvil.yaml"), []byte("prune:\n  protected:\n    - Release/*\n    - keep-me\n"), 0o644))
	cfg, err := LoadProject(dir)
	require.NoError(t, err)
	require.Equal(t, []string{"Release/*", "keep-me"}, cfg.Prune.Protected)
}

func TestLoadProjectRejectsInvalidPrunePattern(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "anvil.yaml"), []byte("prune:\n  protected: ['[']\n"), 0o644))
	_, err := LoadProject(dir)
	require.ErrorContains(t, err, "prune.protected")
}

func TestGlobalPruneProtectedSurvivesSave(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := &GlobalConfig{DefaultBranch: "main", Prune: PruneConfig{Protected: []string{"release/*"}}}
	require.NoError(t, SaveGlobalConfig(cfg))
	loaded, err := LoadGlobal()
	require.NoError(t, err)
	require.Equal(t, []string{"release/*"}, loaded.Prune.Protected)
}

func TestPruneAllowIgnoredLoadsAndSurvivesSave(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "anvil.yaml"), []byte("prune:\n  allow_ignored: ['.env', 'storage']\n"), 0o644))
	cfg, err := LoadProject(dir)
	require.NoError(t, err)
	require.Equal(t, []string{".env", "storage"}, cfg.Prune.AllowIgnored)

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	require.NoError(t, SaveGlobalConfig(&GlobalConfig{DefaultBranch: "main", Prune: PruneConfig{AllowIgnored: []string{".env"}}}))
	loaded, err := LoadGlobal()
	require.NoError(t, err)
	require.Equal(t, []string{".env"}, loaded.Prune.AllowIgnored)
}
