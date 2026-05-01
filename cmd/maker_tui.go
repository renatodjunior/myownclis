package cmd

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type makerView int

const (
	viewMakerHome makerView = iota
	viewMakerCmdlet
)

type makerModel struct {
	view         makerView
	cmdlets      []string
	commands     []*Command
	chains       []*Chain
	activeCmdlet string
	input        string
	status       string
	errMsg       string
}

var (
	makerBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				Padding(0, 1).
				BorderForeground(lipgloss.Color("63"))
	makerPromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("111")).Bold(true)
	makerOkStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	makerErrStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	makerIndexStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Bold(true)
)

func newMakerModel() makerModel {
	m := makerModel{view: viewMakerHome}
	m.cmdlets, _ = ListCmdlets()
	m.chains, _ = ListChains()
	return m
}

func (m makerModel) Init() tea.Cmd { return nil }

func (m makerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyEnter:
			return m.handleInput()
		case tea.KeyBackspace:
			if len(m.input) > 0 {
				m.input = m.input[:len(m.input)-1]
			}
		default:
			m.input += msg.String()
		}
	}
	return m, nil
}

func (m makerModel) handleInput() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.input)
	m.input = ""
	m.status = ""
	m.errMsg = ""

	if input == "exit" || input == "q" {
		return m, tea.Quit
	}
	if input == "/ls" || input == "ls" {
		m.view = viewMakerHome
		m.activeCmdlet = ""
		m.cmdlets, _ = ListCmdlets()
		m.chains, _ = ListChains()
		return m, nil
	}

	name := strings.TrimPrefix(input, "/")
	for _, c := range m.cmdlets {
		if c == name {
			m.activeCmdlet = c
			m.view = viewMakerCmdlet
			m.commands, _ = ListCommands(c)
			return m, nil
		}
	}

	if strings.HasPrefix(input, "/run ") {
		return m.doRun(strings.TrimPrefix(input, "/run "))
	}
	if strings.HasPrefix(input, "/del ") {
		return m.doDel(strings.TrimPrefix(input, "/del "))
	}
	if strings.HasPrefix(input, "/add ") {
		return m.doAdd(strings.TrimPrefix(input, "/add "))
	}
	if strings.HasPrefix(input, "/log ") {
		return m.doLog(strings.TrimPrefix(input, "/log "))
	}

	m.errMsg = "desconhecido: " + input + "   (exit para sair)"
	return m, nil
}

func (m makerModel) doRun(arg string) (makerModel, tea.Cmd) {
	if m.view != viewMakerCmdlet {
		m.errMsg = "selecione um cmdlet primeiro"
		return m, nil
	}
	n, ok := parseIndex(arg, len(m.commands))
	if !ok {
		m.errMsg = fmt.Sprintf("número inválido (1-%d)", len(m.commands))
		return m, nil
	}
	c := m.commands[n]
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
		AppendLog(logName(c.Cmdlet, CommandSlug(c.Cmdlet, c.Command)),
			strings.ToUpper(status), "", time.Now())
		return nil
	})
}

func (m makerModel) doDel(arg string) (makerModel, tea.Cmd) {
	if m.view != viewMakerCmdlet {
		m.errMsg = "selecione um cmdlet primeiro"
		return m, nil
	}
	n, ok := parseIndex(arg, len(m.commands))
	if !ok {
		m.errMsg = fmt.Sprintf("número inválido (1-%d)", len(m.commands))
		return m, nil
	}
	c := m.commands[n]
	slug := CommandSlug(c.Cmdlet, c.Command)
	if err := DeleteCommand(c.Cmdlet, slug); err != nil {
		m.errMsg = "erro: " + err.Error()
		return m, nil
	}
	m.commands, _ = ListCommands(m.activeCmdlet)
	m.cmdlets, _ = ListCmdlets()
	m.status = "deletado: " + c.Command
	return m, nil
}

func (m makerModel) doAdd(arg string) (makerModel, tea.Cmd) {
	parts := strings.Fields(arg)
	if len(parts) < 2 {
		m.errMsg = "uso: /add <cmdlet> <command>"
		return m, nil
	}
	cmdlet := parts[0]
	command := strings.Join(parts[1:], " ")
	c := &Command{Cmdlet: cmdlet, Command: command, Type: "shell", CreatedAt: time.Now(), LastStatus: "never"}
	if err := SaveCommand(c); err != nil {
		m.errMsg = "erro: " + err.Error()
		return m, nil
	}
	m.cmdlets, _ = ListCmdlets()
	if m.activeCmdlet == cmdlet {
		m.commands, _ = ListCommands(cmdlet)
	}
	m.status = "salvo: " + cmdlet + "/" + CommandSlug(cmdlet, command)
	return m, nil
}

func (m makerModel) doLog(arg string) (makerModel, tea.Cmd) {
	if m.view != viewMakerCmdlet {
		m.errMsg = "selecione um cmdlet primeiro"
		return m, nil
	}
	n, ok := parseIndex(arg, len(m.commands))
	if !ok {
		m.errMsg = fmt.Sprintf("número inválido (1-%d)", len(m.commands))
		return m, nil
	}
	c := m.commands[n]
	name := logName(c.Cmdlet, CommandSlug(c.Cmdlet, c.Command))
	lines, _ := TailLog(name, 15)
	m.status = strings.Join(lines, "\n")
	return m, nil
}

func (m makerModel) View() string {
	var b strings.Builder

	summary := ""
	for _, c := range m.cmdlets {
		cmds, _ := ListCommands(c)
		summary += fmt.Sprintf("%s·%d  ", c, len(cmds))
	}
	summary += fmt.Sprintf("chains·%d", len(m.chains))
	b.WriteString(makerBorderStyle.Render("  " + strings.TrimSpace(summary)))
	b.WriteString("\n\n")

	b.WriteString(styleDim.Render("  /ls  /add  /run N  /log N  /del N  exit"))
	b.WriteString("\n\n")

	if m.view == viewMakerCmdlet && m.activeCmdlet != "" {
		b.WriteString(fmt.Sprintf("  %s\n\n", styleHeader.Render("  "+strings.ToUpper(m.activeCmdlet))))
		for i, c := range m.commands {
			status := styleDim.Render("— never")
			if c.LastStatus == "success" && c.LastRun != nil {
				status = makerOkStyle.Render("✓ " + c.LastRun.Format("15:04"))
			} else if c.LastStatus == "failed" {
				status = makerErrStyle.Render("✗")
			}
			desc := ""
			if c.Description != "" {
				desc = "  " + styleDim.Render(c.Description)
			}
			b.WriteString(fmt.Sprintf("  %s  %-40s %s%s\n",
				makerIndexStyle.Render(fmt.Sprintf("[%d]", i+1)),
				styleDim.Render(c.Command),
				status,
				desc,
			))
		}
		b.WriteString("\n")
	}

	if m.status != "" {
		for _, line := range strings.Split(m.status, "\n") {
			b.WriteString(makerOkStyle.Render("  "+line) + "\n")
		}
	}
	if m.errMsg != "" {
		b.WriteString(makerErrStyle.Render("  "+m.errMsg) + "\n")
	}

	prompt := "maker"
	if m.activeCmdlet != "" {
		prompt = "maker [" + m.activeCmdlet + "]"
	}
	b.WriteString(fmt.Sprintf("\n  %s %s", makerPromptStyle.Render(prompt+" ❯"), m.input))
	return b.String()
}

func parseIndex(s string, max int) (int, bool) {
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n); err != nil {
		return 0, false
	}
	if n < 1 || n > max {
		return 0, false
	}
	return n - 1, true
}
