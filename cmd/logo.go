package cmd

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var mocLogoLines = []string{
	"███╗   ███╗  ██████╗   ██████╗",
	"████╗ ████║ ██╔═══██╗ ██╔════╝",
	"██╔████╔██║ ██║   ██║ ██║     ",
	"██║╚██╔╝██║ ██║   ██║ ██║     ",
	"██║ ╚═╝ ██║ ╚██████╔╝ ╚██████╗",
	"╚═╝     ╚═╝  ╚═════╝   ╚═════╝",
}

var logoGradient = []lipgloss.Color{
	"51",
	"45",
	"111",
	"147",
	"183",
	"219",
}

const LogoVisualWidth = 30

func renderLogoLine(i int) string {
	st := lipgloss.NewStyle().Foreground(logoGradient[i]).Bold(true)
	return st.Render(mocLogoLines[i])
}

// RenderLogo returns the full multi-line gradient logo, newline-terminated.
func RenderLogo() string {
	var out strings.Builder
	for i := range mocLogoLines {
		out.WriteString(renderLogoLine(i))
		out.WriteByte('\n')
	}
	return out.String()
}

// LogoLines returns each logo row already styled (no trailing newlines).
func LogoLines() []string {
	out := make([]string, len(mocLogoLines))
	for i := range mocLogoLines {
		out[i] = renderLogoLine(i)
	}
	return out
}
