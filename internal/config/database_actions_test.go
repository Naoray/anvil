package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDatabaseActionSelectorsPreserveExactCase(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "anvil.yaml"), []byte("scaffold:\n  database_actions:\n    MixedCaseDB: migrate\n"), 0600))
	cfg, err := LoadProject(root)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"MixedCaseDB": "migrate"}, cfg.Scaffold.DatabaseActions)
}
