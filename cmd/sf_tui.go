package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sfn"
	"github.com/aws/aws-sdk-go-v2/service/sfn/types"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ══════════════════════════════════════════════════════════════════════════════
// WATCH — dashboard live com split pane
// ══════════════════════════════════════════════════════════════════════════════

type watchMachineRow struct {
	name    string
	arn     string
	running []watchRunningExec
}

type watchRunningExec struct {
	execArn string
	name    string
	elapsed time.Duration
}

type watchModel struct {
	client      *sfn.Client
	ctx         context.Context
	interval    int
	filterArn   string // if set, filter only this machine
	filterName  string
	width       int
	height      int
	loading     bool
	err         error
	data        []watchMachineRow
	spinner     spinner.Model
}

type watchDataMsg []watchMachineRow
type watchTickMsg struct{}
type watchErrMsg struct{ err error }

func newWatchModel(client *sfn.Client, ctx context.Context, interval int, filterArn, filterName string) watchModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	return watchModel{
		client:     client,
		ctx:        ctx,
		interval:   interval,
		filterArn:  filterArn,
		filterName: filterName,
		loading:    true,
		spinner:    s,
	}
}

func (m watchModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, fetchWatchDataCmd(m.client, m.ctx, m.filterArn))
}

func fetchWatchDataCmd(client *sfn.Client, ctx context.Context, filterArn string) tea.Cmd {
	return func() tea.Msg {
		machines, err := fetchAllMachines(ctx, client)
		if err != nil {
			return watchErrMsg{err}
		}
		var rows []watchMachineRow
		for _, m := range machines {
			arn := aws.ToString(m.StateMachineArn)
			if filterArn != "" && arn != filterArn {
				continue
			}
			out, err := client.ListExecutions(ctx, &sfn.ListExecutionsInput{
				StateMachineArn: aws.String(arn),
				StatusFilter:    types.ExecutionStatusRunning,
				MaxResults:      20,
			})
			if err != nil || len(out.Executions) == 0 {
				continue
			}
			row := watchMachineRow{name: aws.ToString(m.Name), arn: arn}
			for _, e := range out.Executions {
				row.running = append(row.running, watchRunningExec{
					execArn: aws.ToString(e.ExecutionArn),
					name:    aws.ToString(e.Name),
					elapsed: time.Since(aws.ToTime(e.StartDate)).Round(time.Second),
				})
			}
			rows = append(rows, row)
		}
		return watchDataMsg(rows)
	}
}

func tickCmd(interval int) tea.Cmd {
	return tea.Tick(time.Duration(interval)*time.Second, func(t time.Time) tea.Msg {
		return watchTickMsg{}
	})
}

func (m watchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

	case watchDataMsg:
		m.data = []watchMachineRow(msg)
		m.loading = false
		return m, tickCmd(m.interval)

	case watchErrMsg:
		m.err = msg.err
		m.loading = false
		return m, nil

	case watchTickMsg:
		m.loading = true
		return m, tea.Batch(m.spinner.Tick, fetchWatchDataCmd(m.client, m.ctx, m.filterArn))

	case spinner.TickMsg:
		if m.loading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m watchModel) View() string {
	now := time.Now().Format("15:04:05")
	refresh := styleDim.Render(fmt.Sprintf("updated %s · %ds", now, m.interval))

	title := styleHeader.Render("  DASHBOARD — RUNNING")
	if m.filterName != "" {
		title = styleHeader.Render("  DASHBOARD — " + m.filterName)
	}
	titleLine := title + "  " + refresh
	if m.loading {
		titleLine += "  " + m.spinner.View()
	}

	if m.err != nil {
		return "\n" + titleLine + "\n\n" + styleError.Render("  Error: "+m.err.Error()) + "\n"
	}

	totalRunning := 0
	for _, r := range m.data {
		totalRunning += len(r.running)
	}

	if totalRunning == 0 {
		empty := styleDim.Render("  No executions in progress.")
		return "\n" + titleLine + "\n\n" + empty + "\n\n" + styleStatusBar.Render("  [q] quit") + "\n"
	}

	leftW := m.width / 3
	rightW := m.width - leftW - 3
	if leftW < 20 {
		leftW = 20
	}
	if rightW < 20 {
		rightW = 20
	}

	var leftLines, rightLines []string
	leftLines = append(leftLines, styleHeader.Render("MACHINES"))
	rightLines = append(rightLines, styleHeader.Render("RUNNING EXECUTIONS"))

	for _, row := range m.data {
		count := styleRunning.Render(fmt.Sprintf("(%d)", len(row.running)))
		leftLines = append(leftLines, styleSuccess.Render("▸ "+truncate(row.name, leftW-5))+" "+count)
		for _, e := range row.running {
			elapsed := styleRunning.Render("● " + e.elapsed.String())
			name := styleDim.Render(truncate(e.name, rightW-20))
			rightLines = append(rightLines, elapsed+"  "+name)
		}
	}

	for len(leftLines) < len(rightLines) {
		leftLines = append(leftLines, "")
	}
	for len(rightLines) < len(leftLines) {
		rightLines = append(rightLines, "")
	}

	leftStyle := lipgloss.NewStyle().Width(leftW).Border(lipgloss.NormalBorder(), false, true, false, false).BorderForeground(lipgloss.Color("240"))
	rightStyle := lipgloss.NewStyle().Width(rightW)

	pane := lipgloss.JoinHorizontal(lipgloss.Top,
		leftStyle.Render(strings.Join(leftLines, "\n")),
		" "+rightStyle.Render(strings.Join(rightLines, "\n")),
	)

	statusText := fmt.Sprintf("  %s running  ·  refresh %ds  ·  [q] quit",
		styleRunning.Render(fmt.Sprintf("%d", totalRunning)),
		m.interval,
	)

	return "\n" + titleLine + "\n\n" + pane + "\n\n" + styleStatusBar.Render(statusText) + "\n"
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func runWatchTUI(client *sfn.Client, ctx context.Context, interval int, filterArn, filterName string) error {
	p := tea.NewProgram(newWatchModel(client, ctx, interval, filterArn, filterName), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
