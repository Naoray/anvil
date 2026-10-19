package scaffold

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/naoray/anvil/internal/config"
)

func TestBorrowOptionWithoutApplicationStepFailsBeforeStateWrites(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{Scaffold: config.ScaffoldConfig{BorrowAppDB: t.TempDir()}}
	err := NewScaffoldManager().RunScaffold(root, "branch", "repo", "site", "laravel-shared-db", nil, cfg, false, false, true)
	require.ErrorContains(t, err, "application db.create")
	_, err = os.Stat(filepath.Join(root, config.LocalStateFile))
	require.True(t, os.IsNotExist(err))
}
