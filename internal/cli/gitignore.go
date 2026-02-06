package cli

import (
	"os"
	"path/filepath"

	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/ui"
)

// checkAnvilLocalGitignore checks if .anvil.local is gitignored and warns if not
func checkAnvilLocalGitignore(worktreePath string) {
	// Check if .anvil.local exists
	localStatePath := filepath.Join(worktreePath, ".anvil.local")
	if _, err := os.Stat(localStatePath); os.IsNotExist(err) {
		return
	}

	ignored, err := git.IsIgnored(worktreePath, ".anvil.local")
	if err == nil && ignored {
		return
	}

	ui.PrintWarning("Add .anvil.local to .gitignore to prevent committing local state")
}
