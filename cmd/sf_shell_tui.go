package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sfn"
	"github.com/aws/aws-sdk-go-v2/service/sfn/types"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── views ─────────────────────────────────────────────────────────────────────

type sfView int

const (
	viewSFMachines sfView = iota
	viewSFExecs
)

// ── list items ────────────────────────────────────────────────────────────────

type sfMachineItem struct {
	name string
	arn  string
}

func (i sfMachineItem) Title() string       { return i.name }
func (i sfMachineItem) Description() string { return truncateMaker(i.arn, 48) }
func (i sfMachineItem) FilterValue() string { return i.name }

type sfExecItem struct{ e types.ExecutionListItem }

func (i sfExecItem) Title() string { return aws.ToString(i.e.Name) }
func (i sfExecItem) Description() string {
	icon := "●"
	switch i.e.Status {
	case types.ExecutionStatusSucceeded:
		icon = "✓"
	case types.ExecutionStatusFailed, types.ExecutionStatusTimedOut, types.ExecutionStatusAborted:
		icon = "✗"
	case types.ExecutionStatusRunning:
		icon = "●"
	}
	return fmt.Sprintf("%s %s — %s", icon, string(i.e.Status), i.e.StartDate.Format("02/01 15:04"))
}
func (i sfExecItem) FilterValue() string { return aws.ToString(i.e.Name) }

// ── msg types ─────────────────────────────────────────────────────────────────

type (
	sfMachinesLoadedMsg []types.StateMachineListItem
	sfExecsLoadedMsg    []types.ExecutionListItem
	sfPreviewMsg        struct{ content string }
	sfActionMsg         struct {
		msg   string
		isErr bool
	}
	sfShellErrMsg struct{ err error }
)

// ── model ─────────────────────────────────────────────────────────────────────

type sfShellModel struct {
	client      *sfn.Client
	ctx         context.Context
	view        sfView
	machines    []types.StateMachineListItem
	execs       []types.ExecutionListItem
	activeMArn  string
	activeMName string
	list        list.Model
	preview     viewport.Model
	input       textinput.Model
	spinner     spinner.Model
	loading     bool
	statusMsg   string
	isErr       bool
	width       int
	height      int
}

func newSFShellModel(client *sfn.Client, ctx context.Context) sfShellModel {
	ti := textinput.New()
	ti.Placeholder = "/history  /input  /rerun  /start  /refresh  /help  exit"
	ti.Focus()
	ti.CharLimit = 256
	ti.Width = 60

	d := list.NewDefaultDelegate()
	d.Styles.SelectedTitle = makerSelTitleStyle
	d.Styles.SelectedDesc = makerSelDescStyle
	d.Styles.NormalTitle = makerNormTitleStyle
	d.Styles.NormalDesc = makerNormDescStyle

	l := list.New(nil, d, 32, 14)
	l.Title = "STATE MACHINES"
	l.Styles.Title = makerListTitleStyle
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)

	vp := viewport.New(48, 14)
	vp.Style = lipgloss.NewStyle().PaddingLeft(1)

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

	return sfShellModel{
		client:  client,
		ctx:     ctx,
		list:    l,
		preview: vp,
		input:   ti,
		spinner: sp,
		loading: true,
		width:   80,
		height:  24,
	}
}

func (m sfShellModel) listW() int    { return m.width * makerListPct / 100 }
func (m sfShellModel) previewW() int { return m.width - m.listW() - 1 }
func (m sfShellModel) contentH() int {
	h := m.height - 10
	if h < 4 {
		h = 4
	}
	return h
}

// ── data fetch commands ───────────────────────────────────────────────────────

func sfFetchMachinesCmd(client *sfn.Client, ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		ms, err := fetchAllMachines(ctx, client)
		if err != nil {
			return sfShellErrMsg{err}
		}
		return sfMachinesLoadedMsg(ms)
	}
}

func sfFetchExecsCmd(client *sfn.Client, ctx context.Context, arn string) tea.Cmd {
	return func() tea.Msg {
		out, err := client.ListExecutions(ctx, &sfn.ListExecutionsInput{
			StateMachineArn: aws.String(arn),
			MaxResults:      30,
		})
		if err != nil {
			return sfShellErrMsg{err}
		}
		return sfExecsLoadedMsg(out.Executions)
	}
}

func sfFetchHistoryCmd(client *sfn.Client, ctx context.Context, execArn string) tea.Cmd {
	return func() tea.Msg {
		out, err := client.GetExecutionHistory(ctx, &sfn.GetExecutionHistoryInput{
			ExecutionArn:         aws.String(execArn),
			IncludeExecutionData: aws.Bool(true),
		})
		if err != nil {
			return sfShellErrMsg{err}
		}
		var b strings.Builder
		b.WriteString("\n")
		b.WriteString(makerPvTitleStyle.Render("  HISTORY") + "\n")
		for _, ev := range out.Events {
			ts := ev.Timestamp.Format("15:04:05")
			icon, styled := iconAndStyle(ev.Type, string(ev.Type))
			detail := detailSummary(ev)
			line := fmt.Sprintf("  %s [%s] %s", icon, makerPvDimStyle.Render(ts), styled)
			if detail != "" {
				line += "  " + makerPvDimStyle.Render(truncateMaker(detail, 40))
			}
			b.WriteString(line + "\n")
		}
		return sfPreviewMsg{content: b.String()}
	}
}

func sfFetchInputCmd(client *sfn.Client, ctx context.Context, execArn string) tea.Cmd {
	return func() tea.Msg {
		desc, err := client.DescribeExecution(ctx, &sfn.DescribeExecutionInput{
			ExecutionArn: aws.String(execArn),
		})
		if err != nil {
			return sfShellErrMsg{err}
		}
		var b strings.Builder
		b.WriteString("\n")
		b.WriteString(makerPvTitleStyle.Render("  EXECUTION") + "\n")
		b.WriteString(makerPvKeyStyle.Render(fmt.Sprintf("  %-10s", "status")) +
			makerPvValStyle.Render(string(desc.Status)) + "\n")
		if desc.StartDate != nil {
			b.WriteString(makerPvKeyStyle.Render(fmt.Sprintf("  %-10s", "started")) +
				makerPvValStyle.Render(desc.StartDate.Format("02/01 15:04:05")) + "\n")
		}
		if desc.StopDate != nil {
			b.WriteString(makerPvKeyStyle.Render(fmt.Sprintf("  %-10s", "ended")) +
				makerPvValStyle.Render(desc.StopDate.Format("02/01 15:04:05")) + "\n")
		}
		b.WriteString("\n")
		b.WriteString(makerPvTitleStyle.Render("  INPUT") + "\n")
		for _, l := range strings.Split(prettyJSON(aws.ToString(desc.Input)), "\n") {
			b.WriteString(makerPvDimStyle.Render("  "+l) + "\n")
		}
		if desc.Output != nil {
			b.WriteString("\n")
			b.WriteString(makerPvTitleStyle.Render("  OUTPUT") + "\n")
			for _, l := range strings.Split(prettyJSON(aws.ToString(desc.Output)), "\n") {
				b.WriteString(makerPvDimStyle.Render("  "+l) + "\n")
			}
		}
		return sfPreviewMsg{content: b.String()}
	}
}

func sfDoRerunCmd(client *sfn.Client, ctx context.Context, execArn string) tea.Cmd {
	return func() tea.Msg {
		desc, err := client.DescribeExecution(ctx, &sfn.DescribeExecutionInput{
			ExecutionArn: aws.String(execArn),
		})
		if err != nil {
			return sfActionMsg{msg: "erro: " + err.Error(), isErr: true}
		}
		out, err := client.StartExecution(ctx, &sfn.StartExecutionInput{
			StateMachineArn: desc.StateMachineArn,
			Input:           desc.Input,
			Name:            aws.String(fmt.Sprintf("rerun-%d", time.Now().Unix())),
		})
		if err != nil {
			return sfActionMsg{msg: "erro: " + err.Error(), isErr: true}
		}
		return sfActionMsg{msg: "rerun ok: " + truncateMaker(aws.ToString(out.ExecutionArn), 60)}
	}
}

func sfDoStartCmd(client *sfn.Client, ctx context.Context, machineArn, input string) tea.Cmd {
	if input == "" {
		input = "{}"
	}
	return func() tea.Msg {
		out, err := client.StartExecution(ctx, &sfn.StartExecutionInput{
			StateMachineArn: aws.String(machineArn),
			Input:           aws.String(input),
			Name:            aws.String(fmt.Sprintf("moc-%d", time.Now().Unix())),
		})
		if err != nil {
			return sfActionMsg{msg: "erro: " + err.Error(), isErr: true}
		}
		return sfActionMsg{msg: "started: " + truncateMaker(aws.ToString(out.ExecutionArn), 60)}
	}
}

// ── list refresh / preview ────────────────────────────────────────────────────

func (m sfShellModel) refreshList() sfShellModel {
	var items []list.Item
	if m.view == viewSFMachines {
		m.list.Title = "STATE MACHINES"
		for _, x := range m.machines {
			items = append(items, sfMachineItem{
				name: aws.ToString(x.Name),
				arn:  aws.ToString(x.StateMachineArn),
			})
		}
	} else {
		m.list.Title = strings.ToUpper(m.activeMName)
		for _, x := range m.execs {
			items = append(items, sfExecItem{e: x})
		}
	}
	m.list.SetItems(items)
	return m
}

func (m sfShellModel) previewContent() string {
	if m.loading {
		return "\n  " + m.spinner.View() + "  " + makerPvDimStyle.Render("carregando...")
	}
	switch m.view {
	case viewSFMachines:
		if mi, ok := m.list.SelectedItem().(sfMachineItem); ok {
			return m.previewMachine(mi)
		}
		return m.previewWelcome()
	case viewSFExecs:
		if ei, ok := m.list.SelectedItem().(sfExecItem); ok {
			return m.previewExec(ei.e)
		}
	}
	return ""
}

func (m sfShellModel) previewWelcome() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(makerPvTitleStyle.Render("  STEP FUNCTIONS") + "\n")
	b.WriteString(makerPvDimStyle.Render("  Browser de state machines e execuções AWS.") + "\n\n")
	b.WriteString(makerPvKeyStyle.Render(fmt.Sprintf("  %-10s", "machines")) +
		makerPvValStyle.Render(fmt.Sprintf("%d", len(m.machines))) + "\n\n")
	b.WriteString(makerPvDimStyle.Render("  ↑↓ selecione uma machine para abrir suas execuções.") + "\n")
	b.WriteString(makerPvDimStyle.Render("  /help para ver todos os comandos disponíveis.") + "\n")
	return b.String()
}

func (m sfShellModel) previewMachine(mi sfMachineItem) string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(makerPvTitleStyle.Render("  "+strings.ToUpper(mi.name)) + "\n")
	b.WriteString(makerPvKeyStyle.Render("  arn  ") +
		makerPvDimStyle.Render(truncateMaker(mi.arn, 50)) + "\n\n")
	b.WriteString(makerPvDimStyle.Render("  Enter para listar execuções desta machine.") + "\n")
	return b.String()
}

func (m sfShellModel) previewExec(e types.ExecutionListItem) string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(makerPvTitleStyle.Render("  "+truncateMaker(aws.ToString(e.Name), 46)) + "\n")
	statusStyle := makerPvDimStyle
	switch e.Status {
	case types.ExecutionStatusSucceeded:
		statusStyle = makerPvOkStyle
	case types.ExecutionStatusFailed, types.ExecutionStatusTimedOut, types.ExecutionStatusAborted:
		statusStyle = makerPvErrStyle
	}
	b.WriteString(makerPvKeyStyle.Render(fmt.Sprintf("  %-10s", "status")) +
		statusStyle.Render(string(e.Status)) + "\n")
	b.WriteString(makerPvKeyStyle.Render(fmt.Sprintf("  %-10s", "started")) +
		makerPvValStyle.Render(e.StartDate.Format("02/01 15:04:05")) + "\n")
	if e.StopDate != nil {
		b.WriteString(makerPvKeyStyle.Render(fmt.Sprintf("  %-10s", "ended")) +
			makerPvValStyle.Render(e.StopDate.Format("02/01 15:04:05")) + "\n")
	}
	b.WriteString(makerPvKeyStyle.Render(fmt.Sprintf("  %-10s", "arn")) +
		makerPvDimStyle.Render(truncateMaker(aws.ToString(e.ExecutionArn), 50)) + "\n")
	b.WriteString("\n")
	b.WriteString(makerPvDimStyle.Render("  /history  /input  /rerun") + "\n")
	return b.String()
}

func (m sfShellModel) helpContent() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(makerPvTitleStyle.Render("  COMANDOS") + "\n")
	cmds := [][]string{
		{"enter", "abrir machine / mostrar history da exec"},
		{"esc", "voltar / limpar input"},
		{"/history", "history completo da execução selecionada"},
		{"/input", "input + output formatados"},
		{"/rerun", "reprocessa execução com mesmo input"},
		{"/start", "inicia nova execução com input vazio {}"},
		{"/refresh", "recarrega lista atual"},
		{"/help", "esta ajuda"},
		{"ls", "voltar para state machines"},
		{"exit / q", "sair do sf"},
	}
	for _, row := range cmds {
		b.WriteString(fmt.Sprintf("  %s  %s\n",
			lipgloss.NewStyle().Foreground(lipgloss.Color("111")).Bold(true).
				Render(fmt.Sprintf("%-12s", row[0])),
			makerPvDimStyle.Render(row[1]),
		))
	}
	b.WriteString("\n")
	b.WriteString(makerPvTitleStyle.Render("  ATALHOS") + "\n")
	shortcuts := [][]string{
		{"↑↓", "navegar lista"},
		{"PgUp/PgDn", "navegar rápido"},
		{"ctrl+c", "sair imediatamente"},
	}
	for _, row := range shortcuts {
		b.WriteString(fmt.Sprintf("  %s  %s\n",
			lipgloss.NewStyle().Foreground(lipgloss.Color("111")).Bold(true).
				Render(fmt.Sprintf("%-12s", row[0])),
			makerPvDimStyle.Render(row[1]),
		))
	}
	return b.String()
}

// ── lifecycle ─────────────────────────────────────────────────────────────────

func (m sfShellModel) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.spinner.Tick, sfFetchMachinesCmd(m.client, m.ctx))
}

// ── update ────────────────────────────────────────────────────────────────────

func (m sfShellModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var inputCmd tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		lw, pw, ch := m.listW(), m.previewW(), m.contentH()
		m.list.SetSize(lw, ch)
		m.preview.Width = pw
		m.preview.Height = ch
		m.input.Width = m.width - 18
		m.preview.SetContent(m.previewContent())
		return m, nil

	case spinner.TickMsg:
		if m.loading {
			var c tea.Cmd
			m.spinner, c = m.spinner.Update(msg)
			return m, c
		}
		return m, nil

	case sfMachinesLoadedMsg:
		m.machines = []types.StateMachineListItem(msg)
		m.loading = false
		m = m.refreshList()
		m.preview.SetContent(m.previewContent())
		return m, nil

	case sfExecsLoadedMsg:
		m.execs = []types.ExecutionListItem(msg)
		m.loading = false
		m = m.refreshList()
		m.preview.SetContent(m.previewContent())
		return m, nil

	case sfPreviewMsg:
		m.preview.SetContent(msg.content)
		m.preview.GotoTop()
		return m, nil

	case sfActionMsg:
		m.statusMsg = msg.msg
		m.isErr = msg.isErr
		return m, nil

	case sfShellErrMsg:
		m.loading = false
		m.statusMsg = "erro: " + msg.err.Error()
		m.isErr = true
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit

		case tea.KeyEsc:
			if m.input.Value() != "" {
				m.input.SetValue("")
				return m, nil
			}
			if m.view == viewSFExecs {
				return m.backToMachines()
			}
			return m, nil

		case tea.KeyEnter:
			val := strings.TrimSpace(m.input.Value())
			m.input.SetValue("")
			m.statusMsg = ""
			m.isErr = false
			if val != "" {
				return m.processCommand(val)
			}
			return m.handleSelect()

		case tea.KeyUp, tea.KeyDown, tea.KeyPgUp, tea.KeyPgDown:
			if m.input.Value() == "" {
				var listCmd tea.Cmd
				m.list, listCmd = m.list.Update(msg)
				m.preview.SetContent(m.previewContent())
				m.preview.GotoTop()
				return m, listCmd
			}
		}
	}

	m.input, inputCmd = m.input.Update(msg)
	return m, inputCmd
}

// ── command dispatch ──────────────────────────────────────────────────────────

func (m sfShellModel) processCommand(input string) (tea.Model, tea.Cmd) {
	switch input {
	case "exit", "q":
		return m, tea.Quit
	case "ls", "/ls":
		mm, c := m.backToMachines()
		return mm, c
	case "/help", "help", "?":
		m.preview.SetContent(m.helpContent())
		m.preview.GotoTop()
		return m, nil
	case "/refresh", "refresh":
		invalidateCache()
		m.loading = true
		if m.view == viewSFMachines {
			return m, tea.Batch(m.spinner.Tick, sfFetchMachinesCmd(m.client, m.ctx))
		}
		return m, tea.Batch(m.spinner.Tick, sfFetchExecsCmd(m.client, m.ctx, m.activeMArn))
	case "/history":
		if e, ok := m.list.SelectedItem().(sfExecItem); ok {
			return m, sfFetchHistoryCmd(m.client, m.ctx, aws.ToString(e.e.ExecutionArn))
		}
		m.statusMsg = "selecione uma execução primeiro"
		m.isErr = true
		return m, nil
	case "/input":
		if e, ok := m.list.SelectedItem().(sfExecItem); ok {
			return m, sfFetchInputCmd(m.client, m.ctx, aws.ToString(e.e.ExecutionArn))
		}
		m.statusMsg = "selecione uma execução primeiro"
		m.isErr = true
		return m, nil
	case "/rerun":
		if e, ok := m.list.SelectedItem().(sfExecItem); ok {
			return m, sfDoRerunCmd(m.client, m.ctx, aws.ToString(e.e.ExecutionArn))
		}
		m.statusMsg = "selecione uma execução primeiro"
		m.isErr = true
		return m, nil
	case "/start":
		if m.view != viewSFExecs {
			m.statusMsg = "abra uma machine primeiro"
			m.isErr = true
			return m, nil
		}
		return m, sfDoStartCmd(m.client, m.ctx, m.activeMArn, "{}")
	}

	m.statusMsg = "desconhecido: " + input
	m.isErr = true
	return m, nil
}

func (m sfShellModel) handleSelect() (tea.Model, tea.Cmd) {
	item := m.list.SelectedItem()
	if item == nil {
		return m, nil
	}
	switch v := item.(type) {
	case sfMachineItem:
		m.view = viewSFExecs
		m.activeMArn = v.arn
		m.activeMName = v.name
		m.execs = nil
		m.loading = true
		m = m.refreshList()
		m.preview.SetContent(m.previewContent())
		return m, tea.Batch(m.spinner.Tick, sfFetchExecsCmd(m.client, m.ctx, v.arn))
	case sfExecItem:
		return m, sfFetchHistoryCmd(m.client, m.ctx, aws.ToString(v.e.ExecutionArn))
	}
	return m, nil
}

func (m sfShellModel) backToMachines() (sfShellModel, tea.Cmd) {
	m.view = viewSFMachines
	m.activeMArn = ""
	m.activeMName = ""
	m.execs = nil
	m = m.refreshList()
	m.preview.SetContent(m.previewContent())
	return m, nil
}

// ── view ──────────────────────────────────────────────────────────────────────

func (m sfShellModel) View() string {
	lw, pw, ch := m.listW(), m.previewW(), m.contentH()

	// header — multi-line gradient logo + meta column
	crumb := "machines"
	if m.activeMName != "" {
		crumb = "machines › " + m.activeMName
	}
	logoLines := LogoLines()
	metaLines := []string{
		"",
		makerHdrLabelStyle.Render("step functions browser"),
		"",
		makerHdrCrumbStyle.Render(crumb),
		makerHdrStatsStyle.Render(fmt.Sprintf("machines:%d  execs:%d", len(m.machines), len(m.execs))),
		"",
	}
	headerRows := make([]string, len(logoLines))
	for i, lin := range logoLines {
		meta := ""
		if i < len(metaLines) {
			meta = metaLines[i]
		}
		headerRows[i] = "  " + lin + "    " + meta
	}
	header := strings.Join(headerRows, "\n")

	sepW := m.width
	if sepW < 1 {
		sepW = 1
	}
	separator := makerDividerStyle.Render(strings.Repeat("─", sepW))

	m.list.SetSize(lw, ch)
	m.preview.Width = pw - 2
	m.preview.Height = ch

	divLines := strings.Repeat("│\n", ch-1) + "│"
	divider := makerDividerStyle.Render(divLines)

	body := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(lw).Height(ch).Render(m.list.View()),
		divider,
		lipgloss.NewStyle().Width(pw).Height(ch).Render(m.preview.View()),
	)

	statusLine := " "
	if m.statusMsg != "" {
		st := makerStatusOkStyle
		if m.isErr {
			st = makerStatusErrStyle
		}
		statusLine = st.Render(m.statusMsg)
	} else if m.loading {
		statusLine = makerStatusOkStyle.Render("  " + m.spinner.View() + " carregando...")
	}

	prompt := "sf"
	if m.activeMName != "" {
		prompt = "sf › " + m.activeMName
	}
	inputLine := fmt.Sprintf("  %s %s",
		makerPromptStyle.Render(prompt+" ❯"),
		m.input.View(),
	)

	var hintLine string
	if m.view == viewSFMachines {
		hintLine = makerHintStyle.Render("↑↓ nav  enter abrir  /refresh  /help  exit")
	} else {
		hintLine = makerHintStyle.Render("↑↓ nav  enter history  /input  /rerun  /start  esc voltar")
	}

	return strings.Join([]string{header, separator, body, statusLine, inputLine, hintLine}, "\n")
}

// ── entry point ───────────────────────────────────────────────────────────────

func runSFShellTUI(client *sfn.Client, ctx context.Context) error {
	p := tea.NewProgram(newSFShellModel(client, ctx), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
