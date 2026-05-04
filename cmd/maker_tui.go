package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── views ─────────────────────────────────────────────────────────────────────

type makerView int

const (
	viewMakerHome makerView = iota
	viewMakerCmdlet
)

// ── list items ────────────────────────────────────────────────────────────────

type makerCmdletItem struct {
	name     string
	cmdCount int
}

func (i makerCmdletItem) Title() string      { return i.name }
func (i makerCmdletItem) Description() string { return fmt.Sprintf("%d commands", i.cmdCount) }
func (i makerCmdletItem) FilterValue() string { return i.name }

type makerCommandItem struct{ cmd *Command }

func (i makerCommandItem) Title() string { return i.cmd.Command }
func (i makerCommandItem) Description() string {
	if i.cmd.LastRun == nil {
		return "never run"
	}
	icon := "✓"
	if i.cmd.LastStatus == "failed" {
		icon = "✗"
	}
	return fmt.Sprintf("%s %s — %s", icon, i.cmd.LastStatus, i.cmd.LastRun.Format("02/01 15:04"))
}
func (i makerCommandItem) FilterValue() string { return i.cmd.Command }

// ── msg types ─────────────────────────────────────────────────────────────────

type makerRefreshMsg struct{ logContent string }

// ── styles ────────────────────────────────────────────────────────────────────

var (
	// header meta (right of logo)
	makerHdrLabelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("244")).
				Italic(true)

	makerHdrCrumbStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("87")).
				Bold(true)

	makerHdrStatsStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("241"))

	// list title
	makerListTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("87")).
				MarginLeft(1)

	// delegate — no extra PaddingLeft; let delegate handle its own cursor indent
	makerSelTitleStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("111")).Bold(true)
	makerSelDescStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	makerNormTitleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	makerNormDescStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	// panel divider
	makerDividerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("237"))

	// preview panel text
	makerPvTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("87")).
				PaddingLeft(2).
				PaddingBottom(1)

	makerPvKeyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			PaddingLeft(2)

	makerPvValStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	makerPvOkStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	makerPvErrStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	makerPvDimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	// footer
	makerStatusOkStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("82")).PaddingLeft(2)
	makerStatusErrStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).PaddingLeft(2)
	makerHintStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("237")).PaddingLeft(2)
	makerPromptStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("111")).Bold(true)
)

// ── layout constants ──────────────────────────────────────────────────────────

const makerListPct = 40 // percent of width for left panel

// ── model ─────────────────────────────────────────────────────────────────────

type makerModel struct {
	view         makerView
	activeCmdlet string
	cmdlets      []string
	chains       []*Chain
	commands     []*Command
	list         list.Model
	preview      viewport.Model
	input        textinput.Model
	statusMsg    string
	isErr        bool
	width        int
	height       int
}

func newMakerModel() makerModel {
	ti := textinput.New()
	ti.Placeholder = "/add  /del  /log  /help  exit"
	ti.Focus()
	ti.CharLimit = 256
	ti.Width = 60

	d := list.NewDefaultDelegate()
	d.Styles.SelectedTitle = makerSelTitleStyle
	d.Styles.SelectedDesc  = makerSelDescStyle
	d.Styles.NormalTitle   = makerNormTitleStyle
	d.Styles.NormalDesc    = makerNormDescStyle

	l := list.New(nil, d, 32, 14)
	l.Title = "CMDLETS"
	l.Styles.Title = makerListTitleStyle
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)

	vp := viewport.New(48, 14)
	vp.Style = lipgloss.NewStyle().PaddingLeft(1)

	m := makerModel{
		view:    viewMakerHome,
		list:    l,
		preview: vp,
		input:   ti,
		width:   80,
		height:  24,
	}
	m.cmdlets, _ = ListCmdlets()
	m.chains, _ = ListChains()
	m = m.withRefreshedList()
	m.preview.SetContent(m.previewContent())
	return m
}

func (m makerModel) listW() int    { return m.width * makerListPct / 100 }
func (m makerModel) previewW() int { return m.width - m.listW() - 1 }
func (m makerModel) contentH() int {
	// logo(6) + separator(1) + status(1) + input(1) + hint(1) = 10
	h := m.height - 10
	if h < 4 {
		return 4
	}
	return h
}

// withRefreshedList rebuilds list items from current state.
func (m makerModel) withRefreshedList() makerModel {
	var items []list.Item
	if m.view == viewMakerHome {
		m.list.Title = "CMDLETS"
		for _, name := range m.cmdlets {
			cmds, _ := ListCommands(name)
			items = append(items, makerCmdletItem{name: name, cmdCount: len(cmds)})
		}
	} else {
		m.list.Title = strings.ToUpper(m.activeCmdlet)
		for _, c := range m.commands {
			items = append(items, makerCommandItem{cmd: c})
		}
	}
	m.list.SetItems(items)
	return m
}

// ── preview content generators ────────────────────────────────────────────────

func (m makerModel) previewContent() string {
	switch m.view {
	case viewMakerHome:
		ci, ok := m.list.SelectedItem().(makerCmdletItem)
		if !ok {
			return m.previewWelcome()
		}
		return m.previewCmdlet(ci.name)
	case viewMakerCmdlet:
		ci, ok := m.list.SelectedItem().(makerCommandItem)
		if !ok {
			return ""
		}
		return m.previewCommand(ci.cmd)
	}
	return ""
}

func (m makerModel) previewWelcome() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(makerPvTitleStyle.Render("  WELCOME") + "\n")
	b.WriteString(makerPvDimStyle.Render("  Personal CLI command repository.") + "\n\n")
	b.WriteString(makerPvKeyStyle.Render(fmt.Sprintf("  %-10s", "cmdlets")) +
		makerPvValStyle.Render(fmt.Sprintf("%d", len(m.cmdlets))) + "\n")
	b.WriteString(makerPvKeyStyle.Render(fmt.Sprintf("  %-10s", "chains")) +
		makerPvValStyle.Render(fmt.Sprintf("%d", len(m.chains))) + "\n\n")
	b.WriteString(makerPvDimStyle.Render("  ↑↓ select a cmdlet to view its commands.") + "\n")
	b.WriteString(makerPvDimStyle.Render("  /help to see all available commands.") + "\n")
	return b.String()
}

func (m makerModel) previewCmdlet(name string) string {
	var b strings.Builder
	cmds, _ := ListCommands(name)
	b.WriteString("\n")
	b.WriteString(makerPvTitleStyle.Render("  " + strings.ToUpper(name)) + "\n")

	if len(cmds) == 0 {
		b.WriteString(makerPvDimStyle.Render("  no commands — use /add to add one") + "\n")
		return b.String()
	}
	for _, c := range cmds {
		icon := makerPvDimStyle.Render("—")
		if c.LastStatus == "success" {
			icon = makerPvOkStyle.Render("✓")
		} else if c.LastStatus == "failed" {
			icon = makerPvErrStyle.Render("✗")
		}
		lastRun := "never"
		if c.LastRun != nil {
			lastRun = c.LastRun.Format("02/01 15:04")
		}
		b.WriteString(fmt.Sprintf("  %s  %-36s  %s\n",
			icon,
			makerPvValStyle.Render(truncateMaker(c.Command, 36)),
			makerPvDimStyle.Render(lastRun),
		))
	}

	if len(m.chains) > 0 {
		b.WriteString("\n")
		b.WriteString(makerPvTitleStyle.Render("  CHAINS") + "\n")
		for _, ch := range m.chains {
			icon := makerPvDimStyle.Render("—")
			if ch.LastStatus == "success" {
				icon = makerPvOkStyle.Render("✓")
			} else if ch.LastStatus == "failed" {
				icon = makerPvErrStyle.Render("✗")
			}
			b.WriteString(fmt.Sprintf("  %s  %-24s  %s\n",
				icon,
				makerPvValStyle.Render(ch.Name),
				makerPvDimStyle.Render(fmt.Sprintf("%d steps", len(ch.Steps))),
			))
		}
	}
	return b.String()
}

func (m makerModel) previewCommand(c *Command) string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(makerPvTitleStyle.Render("  "+truncateMaker(c.Command, 46)) + "\n")

	rows := [][]string{
		{"type", c.Type},
		{"status", c.LastStatus},
	}
	if c.LastRun != nil {
		rows = append(rows, []string{"last run", c.LastRun.Format("02/01/2006 15:04:05")})
	}
	if c.Workdir != "" {
		rows = append(rows, []string{"workdir", truncateMaker(c.Workdir, 32)})
	}
	if c.Schedule.Cron != "" {
		rows = append(rows, []string{"cron", c.Schedule.Cron})
	}
	for _, row := range rows {
		b.WriteString(makerPvKeyStyle.Render(fmt.Sprintf("  %-10s", row[0])) +
			makerPvValStyle.Render(row[1]) + "\n")
	}

	name := logName(c.Cmdlet, CommandSlug(c.Cmdlet, c.Command))
	lines, _ := TailLog(name, 12)
	if len(lines) > 0 {
		b.WriteString("\n")
		b.WriteString(makerPvTitleStyle.Render("  LOG") + "\n")
		for _, l := range reverseLogEntries(lines) {
			b.WriteString(makerPvDimStyle.Render("  "+l) + "\n")
		}
	}
	return b.String()
}

func (m makerModel) helpContent() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(makerPvTitleStyle.Render("  COMMANDS") + "\n")
	cmds := [][]string{
		{"enter", "run / open cmdlet"},
		{"esc", "back / clear input"},
		{"/add <cmd>", "save in active cmdlet"},
		{"/add <c> <cmd>", "save in any cmdlet"},
		{"/del", "delete selected command"},
		{"/log", "log of selected command"},
		{"/help", "this help"},
		{"ls", "back to home"},
		{"exit / q", "leave maker"},
	}
	for _, row := range cmds {
		b.WriteString(fmt.Sprintf("  %s  %s\n",
			lipgloss.NewStyle().Foreground(lipgloss.Color("111")).Bold(true).
				Render(fmt.Sprintf("%-16s", row[0])),
			makerPvDimStyle.Render(row[1]),
		))
	}
	b.WriteString("\n")
	b.WriteString(makerPvTitleStyle.Render("  SHORTCUTS") + "\n")
	shortcuts := [][]string{
		{"↑↓", "navigate list"},
		{"PgUp/PgDn", "fast navigate"},
		{"ctrl+c", "quit immediately"},
	}
	for _, row := range shortcuts {
		b.WriteString(fmt.Sprintf("  %s  %s\n",
			lipgloss.NewStyle().Foreground(lipgloss.Color("111")).Bold(true).
				Render(fmt.Sprintf("%-16s", row[0])),
			makerPvDimStyle.Render(row[1]),
		))
	}
	return b.String()
}

func buildExecPreview(c *Command, lines []string) string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(makerPvTitleStyle.Render("  "+truncateMaker(c.Command, 46)) + "\n")
	statusStyle := makerPvOkStyle
	if c.LastStatus == "failed" {
		statusStyle = makerPvErrStyle
	}
	b.WriteString(makerPvKeyStyle.Render(fmt.Sprintf("  %-10s", "result")) +
		statusStyle.Render(c.LastStatus) + "\n")
	if c.LastRun != nil {
		b.WriteString(makerPvKeyStyle.Render(fmt.Sprintf("  %-10s", "ran at")) +
			makerPvValStyle.Render(c.LastRun.Format("02/01 15:04:05")) + "\n")
	}
	if len(lines) == 0 {
		b.WriteString("\n" + makerPvDimStyle.Render("  no output recorded") + "\n")
		return b.String()
	}
	b.WriteString("\n")
	b.WriteString(makerPvTitleStyle.Render("  OUTPUT LOG") + "\n")
	for _, l := range reverseLogEntries(lines) {
		b.WriteString(makerPvDimStyle.Render("  "+l) + "\n")
	}
	return b.String()
}

// reverseLogEntries flips chronological log lines so newest entry shows first.
// An entry starts with a "[YYYY-MM-DD ..." header and may span multiple lines
// of captured output before the next header.
func reverseLogEntries(lines []string) []string {
	var entries [][]string
	var cur []string
	for _, l := range lines {
		if strings.HasPrefix(l, "[") && len(cur) > 0 {
			entries = append(entries, cur)
			cur = nil
		}
		cur = append(cur, l)
	}
	if len(cur) > 0 {
		entries = append(entries, cur)
	}
	out := make([]string, 0, len(lines))
	for i := len(entries) - 1; i >= 0; i-- {
		out = append(out, entries[i]...)
	}
	return out
}

func truncateMaker(s string, max int) string {
	if max <= 3 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-3]) + "..."
}

// ── lifecycle ─────────────────────────────────────────────────────────────────

func (m makerModel) Init() tea.Cmd {
	return textinput.Blink
}

// ── update ────────────────────────────────────────────────────────────────────

func (m makerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

	case makerRefreshMsg:
		m.cmdlets, _ = ListCmdlets()
		m.chains, _ = ListChains()
		if m.activeCmdlet != "" {
			m.commands, _ = ListCommands(m.activeCmdlet)
		}
		m = m.withRefreshedList()
		if msg.logContent != "" {
			m.preview.SetContent(msg.logContent)
			m.preview.GotoTop()
		} else {
			m.preview.SetContent(m.previewContent())
		}
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
			if m.view == viewMakerCmdlet {
				m.view = viewMakerHome
				m.activeCmdlet = ""
				m.commands = nil
				m.statusMsg = ""
				m.isErr = false
				m.cmdlets, _ = ListCmdlets()
				m.chains, _ = ListChains()
				m = m.withRefreshedList()
				m.preview.SetContent(m.previewContent())
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
			return m.handleListSelect()

		case tea.KeyUp, tea.KeyDown, tea.KeyPgUp, tea.KeyPgDown:
			// route to list when input is empty; preview follows selection
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

func (m makerModel) processCommand(input string) (tea.Model, tea.Cmd) {
	switch input {
	case "exit", "q":
		return m, tea.Quit
	case "ls", "/ls":
		m.view = viewMakerHome
		m.activeCmdlet = ""
		m.commands = nil
		m.cmdlets, _ = ListCmdlets()
		m.chains, _ = ListChains()
		m = m.withRefreshedList()
		m.preview.SetContent(m.previewContent())
		return m, nil
	case "/del", "del":
		return m.doDel()
	case "/log", "log":
		return m.doLog()
	case "/help", "help", "?":
		m.preview.SetContent(m.helpContent())
		m.preview.GotoTop()
		return m, nil
	}

	if m.view == viewMakerHome {
		name := strings.TrimPrefix(input, "/")
		for _, c := range m.cmdlets {
			if strings.EqualFold(c, name) {
				return m.navigateToCmdlet(c)
			}
		}
	}

	if strings.HasPrefix(input, "/add ") {
		return m.doAdd(strings.TrimPrefix(input, "/add "))
	}
	if strings.HasPrefix(input, "add ") {
		return m.doAdd(strings.TrimPrefix(input, "add "))
	}

	m.statusMsg = "unknown: " + input
	m.isErr = true
	return m, nil
}

func (m makerModel) handleListSelect() (tea.Model, tea.Cmd) {
	item := m.list.SelectedItem()
	if item == nil {
		return m, nil
	}
	switch v := item.(type) {
	case makerCmdletItem:
		return m.navigateToCmdlet(v.name)
	case makerCommandItem:
		return m.doRunCommand(v.cmd)
	}
	return m, nil
}

// ── actions ───────────────────────────────────────────────────────────────────

func (m makerModel) navigateToCmdlet(name string) (makerModel, tea.Cmd) {
	m.view = viewMakerCmdlet
	m.activeCmdlet = name
	m.commands, _ = ListCommands(name)
	m = m.withRefreshedList()
	m.preview.SetContent(m.previewContent())
	m.preview.GotoTop()
	return m, nil
}

func (m makerModel) doRunCommand(c *Command) (makerModel, tea.Cmd) {
	m.statusMsg = "running: " + c.Command
	m.isErr = false
	cmdRef := c
	run := func() tea.Msg {
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/C", cmdRef.Command)
		} else {
			cmd = exec.Command("sh", "-c", cmdRef.Command)
		}
		if cmdRef.Workdir != "" {
			cmd.Dir = cmdRef.Workdir
		}
		out, err := cmd.CombinedOutput()
		status := "success"
		if err != nil {
			status = "failed"
		}
		now := time.Now()
		cmdRef.LastRun = &now
		cmdRef.LastStatus = status
		SaveCommand(cmdRef)
		logN := logName(cmdRef.Cmdlet, CommandSlug(cmdRef.Cmdlet, cmdRef.Command))
		AppendLog(logN, strings.ToUpper(status), string(out), time.Now())
		lines, _ := TailLog(logN, 60)
		return makerRefreshMsg{logContent: buildExecPreview(cmdRef, lines)}
	}
	return m, run
}

func (m makerModel) doDel() (makerModel, tea.Cmd) {
	if m.view != viewMakerCmdlet {
		m.statusMsg = "open a cmdlet first"
		m.isErr = true
		return m, nil
	}
	ci, ok := m.list.SelectedItem().(makerCommandItem)
	if !ok {
		return m, nil
	}
	slug := CommandSlug(ci.cmd.Cmdlet, ci.cmd.Command)
	if err := DeleteCommand(ci.cmd.Cmdlet, slug); err != nil {
		m.statusMsg = "error: " + err.Error()
		m.isErr = true
		return m, nil
	}
	m.statusMsg = "deleted: " + ci.cmd.Command
	m.commands, _ = ListCommands(m.activeCmdlet)
	m.cmdlets, _ = ListCmdlets()
	m = m.withRefreshedList()
	m.preview.SetContent(m.previewContent())
	return m, nil
}

func (m makerModel) doAdd(arg string) (makerModel, tea.Cmd) {
	parts := strings.Fields(arg)
	var cmdlet, command string

	if m.view == viewMakerCmdlet && m.activeCmdlet != "" {
		if len(parts) < 1 {
			m.statusMsg = "usage: /add <command>"
			m.isErr = true
			return m, nil
		}
		cmdlet = m.activeCmdlet
		command = arg
	} else {
		if len(parts) < 2 {
			m.statusMsg = "usage: /add <cmdlet> <command>"
			m.isErr = true
			return m, nil
		}
		cmdlet = parts[0]
		command = strings.Join(parts[1:], " ")
	}

	if !strings.HasPrefix(strings.ToLower(command), strings.ToLower(cmdlet)+" ") &&
		!strings.EqualFold(command, cmdlet) {
		command = cmdlet + " " + command
	}

	cwd, _ := os.Getwd()
	c := &Command{
		Cmdlet:     cmdlet,
		Command:    command,
		Type:       "shell",
		Workdir:    cwd,
		CreatedAt:  time.Now(),
		LastStatus: "never",
	}
	if err := SaveCommand(c); err != nil {
		m.statusMsg = "error: " + err.Error()
		m.isErr = true
		return m, nil
	}
	m.statusMsg = "saved: " + cmdlet + "/" + CommandSlug(cmdlet, command)
	m.cmdlets, _ = ListCmdlets()
	m.chains, _ = ListChains()
	if m.activeCmdlet == cmdlet {
		m.commands, _ = ListCommands(cmdlet)
	}
	m = m.withRefreshedList()
	m.preview.SetContent(m.previewContent())
	return m, nil
}

func (m makerModel) doLog() (makerModel, tea.Cmd) {
	if m.view != viewMakerCmdlet {
		m.statusMsg = "open a cmdlet first"
		m.isErr = true
		return m, nil
	}
	ci, ok := m.list.SelectedItem().(makerCommandItem)
	if !ok {
		return m, nil
	}
	name := logName(ci.cmd.Cmdlet, CommandSlug(ci.cmd.Cmdlet, ci.cmd.Command))
	lines, _ := TailLog(name, 20)
	m.preview.SetContent(buildExecPreview(ci.cmd, lines))
	m.preview.GotoTop()
	return m, nil
}

// ── view ──────────────────────────────────────────────────────────────────────

func (m makerModel) View() string {
	lw, pw, ch := m.listW(), m.previewW(), m.contentH()

	// ── header (multi-line logo + meta) ──────────────────────────────────────
	crumb := "home"
	if m.activeCmdlet != "" {
		crumb = "home › " + m.activeCmdlet
	}
	logoLines := LogoLines()
	metaLines := []string{
		"",
		makerHdrLabelStyle.Render("command repository"),
		"",
		makerHdrCrumbStyle.Render(crumb),
		makerHdrStatsStyle.Render(fmt.Sprintf("cmdlets:%d  chains:%d", len(m.cmdlets), len(m.chains))),
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

	// ── panels ────────────────────────────────────────────────────────────────
	// resize for this frame (safe since View() gets a value copy)
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

	// ── status (always 1 line) ────────────────────────────────────────────────
	statusLine := " "
	if m.statusMsg != "" {
		st := makerStatusOkStyle
		if m.isErr {
			st = makerStatusErrStyle
		}
		statusLine = st.Render(m.statusMsg)
	}

	// ── input ─────────────────────────────────────────────────────────────────
	prompt := "maker"
	if m.activeCmdlet != "" {
		prompt = "maker › " + m.activeCmdlet
	}
	inputLine := fmt.Sprintf("  %s %s",
		makerPromptStyle.Render(prompt+" ❯"),
		m.input.View(),
	)

	// ── hints ─────────────────────────────────────────────────────────────────
	var hintLine string
	if m.view == viewMakerHome {
		hintLine = makerHintStyle.Render("↑↓ nav  enter select  /add <cmdlet> <cmd>  /help  exit")
	} else {
		hintLine = makerHintStyle.Render("↑↓ nav  enter run  /del  /log  /add <cmd>  /help  esc back")
	}

	return strings.Join([]string{header, separator, body, statusLine, inputLine, hintLine}, "\n")
}

// runMakerShell launches the bubbletea TUI.
func runMakerShell() error {
	p := tea.NewProgram(newMakerModel(), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
