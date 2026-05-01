package cmd

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
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

func (i makerCmdletItem) Title() string       { return i.name }
func (i makerCmdletItem) Description() string  { return fmt.Sprintf("%d commands", i.cmdCount) }
func (i makerCmdletItem) FilterValue() string  { return i.name }

type makerCommandItem struct {
	cmd *Command
}

func (i makerCommandItem) Title() string { return i.cmd.Command }
func (i makerCommandItem) Description() string {
	if i.cmd.LastRun == nil {
		return "nunca executado"
	}
	icon := "✓"
	if i.cmd.LastStatus == "failed" {
		icon = "✗"
	}
	return fmt.Sprintf("%s %s — %s", icon, i.cmd.LastStatus, i.cmd.LastRun.Format("02/01 15:04"))
}
func (i makerCommandItem) FilterValue() string { return i.cmd.Command }

// ── msg types ─────────────────────────────────────────────────────────────────

type makerRefreshMsg struct{}

// ── styles ────────────────────────────────────────────────────────────────────

var (
	makerListTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("63")).
				MarginLeft(2)

	makerSelectedTitleStyle = lipgloss.NewStyle().
				PaddingLeft(2).
				Foreground(lipgloss.Color("111")).
				Bold(true)

	makerSelectedDescStyle = lipgloss.NewStyle().
				PaddingLeft(2).
				Foreground(lipgloss.Color("240"))

	makerNormalTitleStyle = lipgloss.NewStyle().
				PaddingLeft(4).
				Foreground(lipgloss.Color("252"))

	makerNormalDescStyle = lipgloss.NewStyle().
				PaddingLeft(4).
				Foreground(lipgloss.Color("240"))

	makerStatusBarStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("82")).
				PaddingLeft(2)

	makerErrBarStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("196")).
				PaddingLeft(2)

	makerHintBarStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")).
				PaddingLeft(2)

	makerPromptLabelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("111")).
				Bold(true)
)

// ── model ─────────────────────────────────────────────────────────────────────

type makerModel struct {
	view         makerView
	activeCmdlet string
	cmdlets      []string
	chains       []*Chain
	commands     []*Command
	list         list.Model
	input        textinput.Model
	statusMsg    string
	isErr        bool
	width        int
	height       int
}

func newMakerModel() makerModel {
	ti := textinput.New()
	ti.Placeholder = "enter para executar · /add /del /log · exit"
	ti.Focus()
	ti.CharLimit = 256
	ti.Width = 60

	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = makerSelectedTitleStyle
	delegate.Styles.SelectedDesc  = makerSelectedDescStyle
	delegate.Styles.NormalTitle   = makerNormalTitleStyle
	delegate.Styles.NormalDesc    = makerNormalDescStyle

	l := list.New(nil, delegate, 80, 14)
	l.Title = "MAKER"
	l.Styles.Title = makerListTitleStyle
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)

	m := makerModel{
		view:   viewMakerHome,
		list:   l,
		input:  ti,
		width:  80,
		height: 24,
	}
	m.cmdlets, _ = ListCmdlets()
	m.chains, _ = ListChains()
	return m.withRefreshedList()
}

// withRefreshedList rebuilds list items from current model state.
func (m makerModel) withRefreshedList() makerModel {
	var items []list.Item
	if m.view == viewMakerHome {
		m.list.Title = "MAKER"
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
		listH := msg.Height - 6
		if listH < 4 {
			listH = 4
		}
		m.list.SetSize(msg.Width-2, listH)
		m.input.Width = msg.Width - 16
		return m, nil

	case makerRefreshMsg:
		m.cmdlets, _ = ListCmdlets()
		m.chains, _ = ListChains()
		if m.activeCmdlet != "" {
			m.commands, _ = ListCommands(m.activeCmdlet)
		}
		return m.withRefreshedList(), nil

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
				return m.withRefreshedList(), nil
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
			// route to list when input is empty so arrows navigate items
			if m.input.Value() == "" {
				var listCmd tea.Cmd
				m.list, listCmd = m.list.Update(msg)
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
		return m.withRefreshedList(), nil
	case "/del", "del":
		return m.doDel()
	case "/log", "log":
		return m.doLog()
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

	m.statusMsg = "desconhecido: " + input
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
	return m.withRefreshedList(), nil
}

func (m makerModel) doRunCommand(c *Command) (makerModel, tea.Cmd) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", c.Command)
	} else {
		cmd = exec.Command("sh", "-c", c.Command)
	}
	if c.Workdir != "" {
		cmd.Dir = c.Workdir
	}
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		status := "success"
		if err != nil {
			status = "failed"
		}
		now := time.Now()
		c.LastRun = &now
		c.LastStatus = status
		SaveCommand(c)
		AppendLog(
			logName(c.Cmdlet, CommandSlug(c.Cmdlet, c.Command)),
			strings.ToUpper(status), "", time.Now(),
		)
		return makerRefreshMsg{}
	})
}

func (m makerModel) doDel() (makerModel, tea.Cmd) {
	if m.view != viewMakerCmdlet {
		m.statusMsg = "selecione um cmdlet primeiro (home → cmdlet)"
		m.isErr = true
		return m, nil
	}
	ci, ok := m.list.SelectedItem().(makerCommandItem)
	if !ok {
		return m, nil
	}
	slug := CommandSlug(ci.cmd.Cmdlet, ci.cmd.Command)
	if err := DeleteCommand(ci.cmd.Cmdlet, slug); err != nil {
		m.statusMsg = "erro: " + err.Error()
		m.isErr = true
		return m, nil
	}
	m.statusMsg = "deletado: " + ci.cmd.Command
	m.commands, _ = ListCommands(m.activeCmdlet)
	m.cmdlets, _ = ListCmdlets()
	return m.withRefreshedList(), nil
}

func (m makerModel) doAdd(arg string) (makerModel, tea.Cmd) {
	parts := strings.Fields(arg)
	var cmdlet, command string

	if m.view == viewMakerCmdlet && m.activeCmdlet != "" {
		if len(parts) < 1 {
			m.statusMsg = "uso: /add <command>"
			m.isErr = true
			return m, nil
		}
		cmdlet = m.activeCmdlet
		command = arg
	} else {
		if len(parts) < 2 {
			m.statusMsg = "uso: /add <cmdlet> <command>"
			m.isErr = true
			return m, nil
		}
		cmdlet = parts[0]
		command = strings.Join(parts[1:], " ")
	}

	// normalize: ensure command always starts with cmdlet
	if !strings.HasPrefix(strings.ToLower(command), strings.ToLower(cmdlet)+" ") &&
		!strings.EqualFold(command, cmdlet) {
		command = cmdlet + " " + command
	}

	c := &Command{
		Cmdlet:     cmdlet,
		Command:    command,
		Type:       "shell",
		CreatedAt:  time.Now(),
		LastStatus: "never",
	}
	if err := SaveCommand(c); err != nil {
		m.statusMsg = "erro: " + err.Error()
		m.isErr = true
		return m, nil
	}
	m.statusMsg = "salvo: " + cmdlet + "/" + CommandSlug(cmdlet, command)
	m.cmdlets, _ = ListCmdlets()
	m.chains, _ = ListChains()
	if m.activeCmdlet == cmdlet {
		m.commands, _ = ListCommands(cmdlet)
	}
	return m.withRefreshedList(), nil
}

func (m makerModel) doLog() (makerModel, tea.Cmd) {
	if m.view != viewMakerCmdlet {
		m.statusMsg = "selecione um cmdlet primeiro (home → cmdlet)"
		m.isErr = true
		return m, nil
	}
	ci, ok := m.list.SelectedItem().(makerCommandItem)
	if !ok {
		return m, nil
	}
	name := logName(ci.cmd.Cmdlet, CommandSlug(ci.cmd.Cmdlet, ci.cmd.Command))
	lines, _ := TailLog(name, 15)
	if len(lines) == 0 {
		m.statusMsg = "sem logs para " + ci.cmd.Command
	} else {
		m.statusMsg = strings.Join(lines, "\n")
	}
	return m, nil
}

// ── view ──────────────────────────────────────────────────────────────────────

func (m makerModel) View() string {
	var b strings.Builder

	b.WriteString(m.list.View())
	b.WriteString("\n")

	if m.statusMsg != "" {
		style := makerStatusBarStyle
		if m.isErr {
			style = makerErrBarStyle
		}
		for _, line := range strings.Split(m.statusMsg, "\n") {
			b.WriteString(style.Render(line) + "\n")
		}
	}

	prompt := "maker"
	if m.activeCmdlet != "" {
		prompt = "maker [" + m.activeCmdlet + "]"
	}
	b.WriteString(fmt.Sprintf("  %s %s\n",
		makerPromptLabelStyle.Render(prompt+" ❯"),
		m.input.View(),
	))

	if m.view == viewMakerHome {
		b.WriteString(makerHintBarStyle.Render("↑↓ navegar · enter selecionar · /add <cmdlet> <cmd> · exit"))
	} else {
		b.WriteString(makerHintBarStyle.Render("↑↓ navegar · enter executar · /del · /log · /add <cmd> · esc voltar"))
	}

	return b.String()
}

// runMakerShell launches the bubbletea TUI.
func runMakerShell() error {
	p := tea.NewProgram(newMakerModel(), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
