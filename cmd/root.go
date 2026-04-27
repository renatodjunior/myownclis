package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	figure "github.com/common-nighthawk/go-figure"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var styleVersion = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
var styleTip = lipgloss.NewStyle().Foreground(lipgloss.Color("111"))

var rootCmd = &cobra.Command{
	Use:   "moc",
	Short: "CLI pessoal para AWS e outros serviços",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMainShell()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.AddCommand(sfCmd)
}

func printBanner() {
	fig := figure.NewColorFigure("MyOwnCLI", "big", "cyan", true)
	fig.Print()
	fmt.Println()

	r := viper.GetString("region")
	p := viper.GetString("profile")
	configInfo := r
	if p != "" {
		configInfo += " · " + p
	}
	fmt.Printf("  %s\n", styleVersion.Render("v0.1.0 — "+configInfo))
	fmt.Printf("  %s\n\n", styleTip.Render("'help' para módulos · 'exit' para sair"))
}

func runMainShell() error {
	printBanner()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("moc ❯ ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		sub := parts[0]
		sargs := parts[1:]

		switch sub {
		case "exit", "quit", "q":
			fmt.Printf("\n  %s\n\n", styleVersion.Render("Até mais!"))
			return nil
		case "help", "?":
			printMainHelp()
		case "sf":
			if len(sargs) == 0 {
				if err := runSFShell(); err != nil {
					fmt.Println(styleError.Render("  Erro: " + err.Error()))
				}
			} else {
				sfCmd.SetArgs(sargs)
				if err := sfCmd.Execute(); err != nil {
					fmt.Println(styleError.Render("  Erro: " + err.Error()))
				}
			}
		default:
			fmt.Printf("  %s %s\n  %s\n",
				styleError.Render("Módulo desconhecido:"), styleVersion.Render(sub),
				styleTip.Render("Digite 'help' para módulos disponíveis"),
			)
		}
	}
	return nil
}

func printMainHelp() {
	modules := [][]string{
		{"sf", "AWS Step Functions — browser, watch, tail, rerun e mais"},
	}
	fmt.Println()
	fmt.Printf("  %s\n\n", styleHeader.Render("MÓDULOS DISPONÍVEIS"))
	for _, m := range modules {
		fmt.Printf("  %-15s %s\n", styleSuccess.Render(m[0]), styleDim.Render(m[1]))
	}
	fmt.Printf("\n  %s\n\n", styleDim.Render("Uso: <módulo> [comando] [args]  ou  <módulo> para entrar no shell"))

	r := viper.GetString("region")
	p := viper.GetString("profile")
	fmt.Printf("  %s  region=%s  profile=%s\n\n",
		styleDim.Render("Config atual:"),
		styleSuccess.Render(r),
		styleSuccess.Render(func() string {
			if p == "" {
				return "default"
			}
			return p
		}()),
	)
}
