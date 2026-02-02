package cli

import (
	"os"
	"path/filepath"

	"github.com/michaeldyrynda/arbor/internal/git"
	"github.com/michaeldyrynda/arbor/internal/ui"
)

// checkArborLocalGitignore checks if .arbor.local is gitignored and warns if not
func checkArborLocalGitignore(worktreePath string) {
	// Check if .arbor.local exists
	localStatePath := filepath.Join(worktreePath, ".arbor.local")
	if _, err := os.Stat(localStatePath); os.IsNotExist(err) {
		return
	}

	ignored, err := git.IsIgnored(worktreePath, ".arbor.local")
	if err == nil && ignored {
		return
	}

	ui.PrintWarning("Add .arbor.local to .gitignore to prevent committing local state")
}
