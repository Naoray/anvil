package cli

import "path/filepath"

func worktreeSiteName(worktreePath, branch, defaultBranch, projectSiteName string) string {
	if branch == defaultBranch && projectSiteName != "" {
		return projectSiteName
	}
	return filepath.Base(worktreePath)
}
