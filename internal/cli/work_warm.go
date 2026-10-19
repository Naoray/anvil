package cli

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/naoray/anvil/internal/git"
)

func resolveWarmSource(pc *ProjectContext, query string) (string, error) {
	worktrees, err := git.ListWorktrees(pc.GitDir)
	if err != nil {
		return "", fmt.Errorf("listing warm sources: %w", err)
	}
	source := ""
	for _, wt := range worktrees {
		if wt.Branch == query || filepath.Base(wt.Path) == query || wt.Path == query {
			if source != "" {
				return "", fmt.Errorf("ambiguous warm source %q; use a registered path or exact branch", query)
			}
			source = wt.Path
		}
	}
	if source == "" {
		return "", fmt.Errorf("warm source %q is not a registered worktree (use exact branch, folder name or path)", query)
	}
	source, err = filepath.EvalSymlinks(source)
	if err != nil {
		return "", fmt.Errorf("warm source unavailable: %w", err)
	}
	common := func(path string) (string, error) {
		cmd := exec.Command("git", "-C", path, "rev-parse", "--path-format=absolute", "--git-common-dir")
		out, err := cmd.Output()
		if err != nil {
			return "", err
		}
		return filepath.EvalSymlinks(strings.TrimSpace(string(out)))
	}
	expected, err := common(pc.ProjectPath)
	if err != nil {
		return "", fmt.Errorf("linked repository: %w", err)
	}
	actual, err := common(source)
	if err != nil {
		return "", fmt.Errorf("warm source repository: %w", err)
	}
	if expected != actual {
		return "", fmt.Errorf("warm source must be a registered worktree of the linked repository")
	}
	return source, nil
}
