package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/artisanexperiences/arbor/internal/git"
)

func RenderTable(headers []string, rows [][]string) string {
	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(Primary)).
		Headers(headers...).
		StyleFunc(func(row, col int) lipgloss.Style {
			return lipgloss.NewStyle().Padding(0, 1)
		})

	for _, row := range rows {
		t.Row(row...)
	}

	return t.String()
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
		Render("🌳 Arbor Worktrees")

	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(Primary)).
		Headers("WORKTREE", "BRANCH", "STATUS").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == 0 {
				return lipgloss.NewStyle().
					Bold(true).
					Foreground(Primary).
					Padding(0, 1)
			}
			if row > 0 && row-1 < len(worktrees) && worktrees[row-1].IsCurrent {
				return lipgloss.NewStyle().
					Bold(true).
					Padding(0, 1)
			}
			return lipgloss.NewStyle().Padding(0, 1)
		})

	var mergedCount int
	for _, wt := range worktrees {
		worktreeName := filepath.Base(wt.Path)
		status := formatWorktreeStatus(wt)
		t.Row(worktreeName, wt.Branch, status)
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
