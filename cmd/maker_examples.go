package cmd

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var makerExamplesCmd = &cobra.Command{
	Use:     "examples",
	Aliases: []string{"tutorial", "ex"},
	Short:   "Hands-on tutorial: save, chain, schedule",
	RunE: func(_ *cobra.Command, _ []string) error {
		printMakerExamples()
		return nil
	},
}

func printMakerExamples() {
	h := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("87"))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	cyan := lipgloss.NewStyle().Foreground(lipgloss.Color("111"))
	cmd := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	tip := lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Italic(true)

	fmt.Println()
	fmt.Println(h.Render("  MOC MAKER — quick tutorial"))
	fmt.Println()

	step := func(n int, title string) {
		fmt.Printf("\n  %s %s\n",
			cyan.Render(fmt.Sprintf("%d.", n)),
			h.Render(title),
		)
	}
	line := func(s string) {
		fmt.Printf("    %s\n", cmd.Render("$ "+s))
	}
	note := func(s string) {
		fmt.Printf("    %s\n", dim.Render(s))
	}

	step(1, "Save commands (workdir auto-captured)")
	line(`moc maker --add git status`)
	line(`moc maker --add "git log --oneline -10"`)
	line(`moc maker --add "git branch -a"`)
	note(`cmdlet = first word of the command. slug = remaining words joined by '-'.`)
	note(`example: "git log --oneline -10" → slug "log-oneline-10"`)

	step(2, "List + inspect")
	line(`moc maker ls`)
	line(`moc maker          # interactive TUI: ↑↓ navigate, enter open, /help for more`)

	step(3, "Run a saved command by slug")
	line(`moc maker run git status`)
	line(`moc maker run git log-oneline-10`)

	step(4, "Build a chain (runs in order, stops on first error)")
	line(`moc maker chain add git-check git/status git/log-oneline-10 git/branch-a`)
	line(`moc maker chain run git-check`)

	step(5, "Export a chain as a standalone bash script")
	line(`moc maker chain export git-check > check.sh`)
	line(`chmod +x check.sh && ./check.sh`)

	step(6, "Schedule a command — easy mode")
	note(`Friendly expressions translate to cron automatically:`)
	fmt.Println()
	for _, row := range [][2]string{
		{`--cron "every 5m"`, `*/5 * * * *`},
		{`--cron "every 15 minutes"`, `*/15 * * * *`},
		{`--cron "hourly"`, `0 * * * *`},
		{`--cron "every 2h"`, `0 */2 * * *`},
		{`--cron "daily"`, `0 0 * * *`},
		{`--cron "daily at 9am"`, `0 9 * * *`},
		{`--cron "daily at 14:30"`, `30 14 * * *`},
		{`--cron "weekdays at 9am"`, `0 9 * * 1-5`},
		{`--cron "weekends at 10am"`, `0 10 * * 0,6`},
	} {
		fmt.Printf("    %s   %s  %s\n",
			cmd.Render(fmt.Sprintf("%-30s", row[0])),
			dim.Render("→"),
			cyan.Render(row[1]),
		)
	}
	fmt.Println()
	note(`Or use raw 5-field cron: --cron "min hour dom month dow"`)
	note(`Example presets also work: --cron "@hourly", "@daily", "@weekly"`)
	fmt.Println()

	step(7, "Apply a schedule")
	line(`moc maker schedule git status --cron "every 15m"`)
	line(`moc maker schedule git status --cron "every 15m" --os    # also register in cron/schtasks`)
	note(`In-session: notifications appear in moc shell while running.`)
	note(`--os: persists across sessions via cron (Linux/macOS) or schtasks (Windows).`)

	step(8, "Unschedule")
	line(`moc maker unschedule git status`)
	line(`moc maker unschedule git status --os`)

	step(9, "Backup / restore")
	line(`moc maker backup`)
	line(`moc maker restore ~/.moc/backup/2026-05-03.yaml`)

	fmt.Println()
	fmt.Printf("  %s %s\n",
		tip.Render("tip:"),
		dim.Render(`use 'moc maker' for the interactive TUI — same actions, plus inline /add /del /log /help`),
	)
	fmt.Println()
}
