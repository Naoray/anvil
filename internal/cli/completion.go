package cli

import (
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/git"
)

// completeWorktreeNames provides shell completion for worktree arguments.
func completeWorktreeNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	pc, err := OpenProjectFromCWD()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	return completeWorktreeNamesFromGitDir(pc.GitDir, cmd, args, toComplete)
}

// completeWorktreeNamesFromGitDir is the testable inner function.
func completeWorktreeNamesFromGitDir(gitDir string, _ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	worktrees, err := git.ListWorktrees(gitDir)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var completions []string
	for _, wt := range worktrees {
		folderName := filepath.Base(wt.Path)
		completions = append(completions, folderName)
	}

	return completions, cobra.ShellCompDirectiveNoFileComp
}
