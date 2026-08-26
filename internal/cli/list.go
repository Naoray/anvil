package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/ui"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all worktrees",
	Long: `List all worktrees in the repository with their status.

Shows worktrees with merge status, current worktree indicator,
and main branch highlighting.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		pc, err := OpenProjectFromCWD()
		if err != nil {
			return err
		}

		jsonOutput := mustGetBool(cmd, "json")
		porcelain := mustGetBool(cmd, "porcelain")
		sortBy := mustGetString(cmd, "sort-by")
		reverse := mustGetBool(cmd, "reverse")

		worktrees, err := git.ListWorktreesDetailed(pc.GitDir, pc.CWD, pc.DefaultBranch)
		if err != nil {
			return fmt.Errorf("listing worktrees: %w", err)
		}

		worktrees = git.SortWorktrees(worktrees, sortBy, reverse)

		if jsonOutput {
			return printJSON(os.Stdout, worktrees)
		}

		if porcelain {
			return printPorcelain(os.Stdout, worktrees)
		}

		return printTable(os.Stdout, worktrees)
	},
}

func printTable(w io.Writer, worktrees []git.Worktree) error {
	if len(worktrees) == 0 {
		_, err := fmt.Fprintln(w, "No worktrees found.")
		return err
	}

	_, err := fmt.Fprintln(w, ui.RenderWorktreeTable(worktrees))
	return err
}

func printJSON(w io.Writer, worktrees []git.Worktree) error {
	type worktreeJSON struct {
		Path       string `json:"path"`
		Branch     string `json:"branch"`
		IsBare     bool   `json:"isBare"`
		IsDetached bool   `json:"isDetached"`
		IsLocked   bool   `json:"isLocked"`
		LockReason string `json:"lockReason,omitempty"`
		IsMain     bool   `json:"isMain"`
		IsCurrent  bool   `json:"isCurrent"`
		IsMerged   bool   `json:"isMerged"`
	}

	jsonWorktrees := make([]worktreeJSON, len(worktrees))
	for i, wt := range worktrees {
		jsonWorktrees[i] = worktreeJSON{
			Path:       wt.Path,
			Branch:     wt.Branch,
			IsBare:     wt.Bare,
			IsDetached: wt.Detached,
			IsLocked:   wt.Locked,
			LockReason: wt.LockReason,
			IsMain:     wt.IsMain,
			IsCurrent:  wt.IsCurrent,
			IsMerged:   wt.IsMerged,
		}
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(jsonWorktrees)
}

func printPorcelain(w io.Writer, worktrees []git.Worktree) error {
	for _, wt := range worktrees {
		current := ""
		if wt.IsCurrent {
			current = "current"
		}

		main := ""
		if wt.IsMain {
			main = "main"
		}

		merged := ""
		if wt.IsMerged {
			merged = "merged"
		} else {
			merged = "-"
		}

		fields := []string{wt.Path, worktreeBranchLabel(wt), main, current, merged}
		if wt.Bare {
			fields = append(fields, "bare")
		}
		if wt.Detached {
			fields = append(fields, "detached")
		}
		if wt.Locked {
			locked := "locked"
			if wt.LockReason != "" {
				locked += "=" + url.PathEscape(wt.LockReason)
			}
			fields = append(fields, locked)
		}

		if _, err := fmt.Fprintln(w, strings.Join(fields, " ")); err != nil {
			return err
		}
	}

	return nil
}

func worktreeBranchLabel(wt git.Worktree) string {
	if wt.Bare {
		return "(bare)"
	}
	if wt.Detached {
		return "(detached)"
	}
	return wt.Branch
}

func init() {
	rootCmd.AddCommand(listCmd)

	listCmd.Flags().Bool("json", false, "Output as JSON array")
	listCmd.Flags().Bool("porcelain", false, "Machine-parseable output")
	listCmd.Flags().String("sort-by", "name", "Sort by: name, branch, created")
	listCmd.Flags().Bool("reverse", false, "Reverse sort order")
	listCmd.MarkFlagsMutuallyExclusive("json", "porcelain")
}
