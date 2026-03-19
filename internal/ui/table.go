package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/charmbracelet/x/term"

	"github.com/naoray/anvil/internal/git"
)

func termWidth() int {
	w, _, err := term.GetSize(os.Stdout.Fd())
	if err != nil || w <= 0 {
		return 120
	}
	return w
}

func truncate(s string, maxLen int) string {
	if maxLen < 4 {
		maxLen = 4
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}


func RenderStatusTable(rows [][]string) string {
	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(Primary)).
		Headers("TOOL", "STATUS", "VERSION").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == 0 {
				return lipgloss.NewStyle().
					Bold(true).
					Foreground(Primary).
					Padding(0, 1)
			}
			if col == 1 {
				return lipgloss.NewStyle().
					Foreground(ColorSuccess).
					Padding(0, 1)
			}
			return lipgloss.NewStyle().Padding(0, 1)
		})

	for _, row := range rows {
		t.Row(row...)
	}

	return fmt.Sprintf("\n%s\n", t.String())
}

func RenderWorktreeTable(worktrees []git.Worktree) string {
	title := lipgloss.NewStyle().
		Foreground(Primary).
		Bold(true).
		Padding(0, 1).
		Render("🌳 Anvil Worktrees")

	// Reserve space: STATUS col fixed width + 4 border chars (│) + 6 padding chars (space per side × 3 cols)
	const (
		statusColWidth = 20 // fits "● current ○ active" + 1 char margin
		tableOverhead  = 10 // 4 × │ + 6 × space padding
	)
	tw := termWidth()
	remaining := tw - statusColWidth - tableOverhead
	if remaining < 20 {
		remaining = 20
	}
	worktreeMax := remaining * 2 / 5
	branchMax := remaining - worktreeMax

	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(Primary)).
		BorderRow(false).
		Headers("WORKTREE", "BRANCH", "STATUS").
		StyleFunc(func(row, col int) lipgloss.Style {
			base := lipgloss.NewStyle().Padding(0, 1)
			if row == 0 {
				return base.Bold(true).Foreground(Primary)
			}
			if row > 0 && row-1 < len(worktrees) && worktrees[row-1].IsCurrent {
				return base.Bold(true)
			}
			return base
		})

	var mergedCount int
	for _, wt := range worktrees {
		worktreeName := truncate(filepath.Base(wt.Path), worktreeMax)
		branch := truncate(wt.Branch, branchMax)
		status := formatWorktreeStatus(wt)
		t.Row(worktreeName, branch, status)
		if wt.IsMerged && !wt.IsMain {
			mergedCount++
		}
	}

	summary := ""
	if len(worktrees) == 1 {
		summary = "1 worktree"
	} else {
		summary = fmt.Sprintf("%d worktrees", len(worktrees))
	}
	if mergedCount > 0 {
		if mergedCount == 1 {
			summary += " • 1 merged"
		} else {
			summary += fmt.Sprintf(" • %d merged", mergedCount)
		}
	}

	summaryStyle := lipgloss.NewStyle().
		Foreground(ColorMuted).
		Padding(0, 1)

	return title + "\n\n" + t.String() + "\n" + summaryStyle.Render(summary)
}

func formatWorktreeStatus(wt git.Worktree) string {
	var parts []string

	if wt.IsCurrent {
		parts = append(parts, CurrentWorktreeStyle.Render("● current"))
	}
	if wt.IsMain {
		parts = append(parts, MainWorktreeStyle.Render("★ main"))
	} else if wt.IsMerged {
		parts = append(parts, MutedStyle.Render("✓ merged"))
	} else {
		parts = append(parts, MutedStyle.Render("○ active"))
	}

	return strings.Join(parts, " ")
}
