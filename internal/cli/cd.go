package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/artisanexperiences/arbor/internal/config"
	"github.com/artisanexperiences/arbor/internal/git"
)

var cdCmd = &cobra.Command{
	Use:   "cd [WORKTREE]",
	Short: "Print the path to a worktree for shell navigation",
	Long: `Prints the path to a worktree, enabling easy shell navigation.

Arguments:
  WORKTREE  Name of the worktree (folder name, branch name, or partial match)
            If omitted, lists all available worktrees

Usage with shell:
  # Print path only
  arbor cd feature-auth

  # Use with cd (bash/zsh)
  cd $(arbor cd feature-auth)

  # Or create a shell function in ~/.zshrc:
  awt() { cd $(arbor cd "$1"); }

  # Then use:
  awt feature-auth

Examples:
  arbor cd                    # List all worktrees
  arbor cd feature-auth       # Print path to feature-auth worktree
  arbor cd auth               # Partial match (if unambiguous)
  arbor cd feature/auth       # Match by branch name`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		pc, err := OpenProjectFromCWD()
		if err != nil {
			return err
		}

		shell := mustGetBool(cmd, "shell")

		// If no argument, list worktrees
		if len(args) == 0 {
			return listWorktreesForCd(os.Stdout, pc.BarePath, pc.IsLinked)
		}

		query := args[0]

		// Find the worktree
		path, err := findWorktreePath(pc.ProjectPath, pc.BarePath, query, pc.IsLinked, pc.GlobalConfig)
		if err != nil {
			return err
		}

		// Output the path
		fmt.Println(formatCdOutput(path, shell))
		return nil
	},
}

// findWorktreePath finds a worktree by folder name, branch name, or partial match
func findWorktreePath(projectPath, barePath, query string, isLinked bool, globalCfg *config.GlobalConfig) (string, error) {
	var worktrees []git.Worktree
	var err error

	if isLinked {
		worktrees, err = git.ListWorktreesFromGitDir(barePath)
	} else {
		worktrees, err = git.ListWorktrees(barePath)
	}
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

// formatCdOutput formats the output path, optionally with shell cd prefix
func formatCdOutput(path string, shell bool) string {
	if shell {
		return "cd " + path
	}
	return path
}

// listWorktreesForCd lists all worktrees in a format suitable for cd selection
func listWorktreesForCd(w io.Writer, barePath string, isLinked bool) error {
	var worktrees []git.Worktree
	var err error

	if isLinked {
		worktrees, err = git.ListWorktreesFromGitDir(barePath)
	} else {
		worktrees, err = git.ListWorktrees(barePath)
	}
	if err != nil {
		return fmt.Errorf("listing worktrees: %w", err)
	}

	if len(worktrees) == 0 {
		fmt.Fprintln(w, "No worktrees found")
		return nil
	}

	fmt.Fprintln(w, "Available worktrees:")
	for _, wt := range worktrees {
		folderName := filepath.Base(wt.Path)
		if folderName == wt.Branch {
			fmt.Fprintf(w, "  %s\n", folderName)
		} else {
			fmt.Fprintf(w, "  %s (%s)\n", folderName, wt.Branch)
		}
	}

	return nil
}

func init() {
	rootCmd.AddCommand(cdCmd)

	cdCmd.Flags().Bool("shell", false, "Output as shell command (cd /path)")
}
