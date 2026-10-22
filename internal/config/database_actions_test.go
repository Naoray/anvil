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

// fresh rebuilds a testing database that checks may be using, so it is only
// accepted as a one-off CLI flag, never as a persistent declaration that
// every ensure would repeat.
func TestDatabaseActionFreshRejectedInConfig(t *testing.T) {
	for _, selector := range []string{"testing", "app_exact"} {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "anvil.yaml"), []byte("scaffold:\n  database_actions:\n    "+selector+": fresh\n"), 0600))
		_, err := LoadProject(root)
		require.ErrorContains(t, err, "database_actions")
		require.ErrorContains(t, err, "--db-action testing=fresh")
	}
}
