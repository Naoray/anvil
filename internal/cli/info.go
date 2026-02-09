package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/git"
)

var infoCmd = &cobra.Command{
	Use:   "info [WORKTREE]",
	Short: "Print the path to a worktree",
	Long: `Prints the path to a worktree.

Arguments:
  WORKTREE  Name of the worktree (folder name, branch name, or partial match)
            If omitted, lists all available worktrees

Examples:
  anvil info                    # List all worktrees
  anvil info feature-auth       # Print path to feature-auth worktree
  anvil info auth               # Partial match (if unambiguous)
  anvil info feature/auth       # Match by branch name`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		pc, err := OpenProjectFromCWD()
		if err != nil {
			return err
		}

		// If no argument, list worktrees
		if len(args) == 0 {
			return listWorktreesForInfo(os.Stdout, pc.GitDir)
		}

		query := args[0]

		// Find the worktree
		path, err := findWorktreePath(pc.GitDir, query)
		if err != nil {
			return err
		}

		fmt.Println(path)
		return nil
	},
}

// findWorktreePath finds a worktree by folder name, branch name, or partial match
func findWorktreePath(gitDir, query string) (string, error) {
	worktrees, err := git.ListWorktrees(gitDir)
	if err != nil {
		return "", fmt.Errorf("listing worktrees: %w", err)
	}

	var matches []git.Worktree

	// First pass: exact matches (folder name or branch name)
	for _, wt := range worktrees {
		folderName := filepath.Base(wt.Path)
		if folderName == query || wt.Branch == query {
			return wt.Path, nil
		}
	}

	// Second pass: partial matches
	queryLower := strings.ToLower(query)
	for _, wt := range worktrees {
		folderName := filepath.Base(wt.Path)
		folderLower := strings.ToLower(folderName)
		branchLower := strings.ToLower(wt.Branch)

		if strings.Contains(folderLower, queryLower) || strings.Contains(branchLower, queryLower) {
			matches = append(matches, wt)
		}
	}

	if len(matches) == 0 {
		return "", fmt.Errorf("no worktree found matching '%s'", query)
	}

	if len(matches) > 1 {
		var names []string
		for _, m := range matches {
			names = append(names, filepath.Base(m.Path))
		}
		return "", fmt.Errorf("multiple worktrees match '%s': %s", query, strings.Join(names, ", "))
	}

	return matches[0].Path, nil
}

// listWorktreesForInfo lists all worktrees in a format suitable for selection
func listWorktreesForInfo(w io.Writer, gitDir string) error {
	worktrees, err := git.ListWorktrees(gitDir)
	if err != nil {
		return fmt.Errorf("listing worktrees: %w", err)
	}

	if len(worktrees) == 0 {
		_, _ = fmt.Fprintln(w, "No worktrees found")
		return nil
	}

	_, _ = fmt.Fprintln(w, "Available worktrees:")
	for _, wt := range worktrees {
		folderName := filepath.Base(wt.Path)
		if folderName == wt.Branch {
			_, _ = fmt.Fprintf(w, "  %s\n", folderName)
		} else {
			_, _ = fmt.Fprintf(w, "  %s (%s)\n", folderName, wt.Branch)
		}
	}

	return nil
}

func init() {
	rootCmd.AddCommand(infoCmd)
}
