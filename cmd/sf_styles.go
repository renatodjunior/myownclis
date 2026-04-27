package cmd

import "github.com/charmbracelet/lipgloss"

var (
	styleHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("33"))
	styleSuccess = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	styleError   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	styleWarning = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	styleDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styleARN     = lipgloss.NewStyle().Foreground(lipgloss.Color("111"))
	styleBanner  = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("213")).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("99")).
			Padding(0, 2)
	styleNumber  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))
	styleRunning = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	stylePick    = lipgloss.NewStyle().Foreground(lipgloss.Color("45"))
	styleStatusBar = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			Background(lipgloss.Color("235")).
			Padding(0, 1)
	styleTableHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("33")).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("240")).
			BorderBottom(true)
	styleTableSelected = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("212")).
			Background(lipgloss.Color("57"))
)
