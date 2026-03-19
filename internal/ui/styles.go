package ui

import "github.com/charmbracelet/lipgloss"

var (
	Primary   = lipgloss.Color("#4CAF50")
	Secondary = lipgloss.Color("#A1887F")

	ColorSuccess = lipgloss.Color("#66BB6A")
	ColorWarning = lipgloss.Color("#FFA726")
	ColorError   = lipgloss.Color("#EF5350")
	ColorInfo    = lipgloss.Color("#29B6F6")
	ColorMuted = lipgloss.AdaptiveColor{Dark: "#9E9E9E", Light: "#555555"}

	Text    = lipgloss.AdaptiveColor{Dark: "#F9FAFB", Light: "#111111"}
	TextDim = lipgloss.AdaptiveColor{Dark: "#9CA3AF", Light: "#6B7280"}
)

var (
	HeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Primary).
			MarginBottom(1)

	SuccessBadge = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#000")).
			Background(ColorSuccess).
			Padding(0, 1).
			Bold(true)

	WarningBadge = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#000")).
			Background(ColorWarning).
			Padding(0, 1).
			Bold(true)

	ErrorBadge = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFF")).
			Background(ColorError).
			Padding(0, 1).
			Bold(true)

	BoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Primary).
			Padding(1, 2)

	MutedStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	CodeStyle = lipgloss.NewStyle().
			Foreground(ColorInfo).
			Bold(true)

	InfoBadge = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#000")).
			Background(ColorInfo).
			Padding(0, 1).
			Bold(true)

	MainWorktreeStyle = lipgloss.NewStyle().
				Foreground(Secondary).
				Bold(true)

	CurrentWorktreeStyle = lipgloss.NewStyle().
				Foreground(ColorSuccess).
				Bold(true)
)
