package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sfn"
	"github.com/aws/aws-sdk-go-v2/service/sfn/types"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// ── flags ─────────────────────────────────────────────────────────────────────

var (
	region       string
	profile      string
	inputJSON    string
	maxResults   int32
	watchSecs    int
	jsonOutput   bool
	statusFilter string
	noConfirm    bool
)

// ── contexto de sessão ────────────────────────────────────────────────────────

type shellSession struct {
	client  *sfn.Client
	ctx     context.Context
	machine *types.StateMachineListItem // machine selecionada
}

func (s *shellSession) machineName() string {
	if s.machine == nil {
		return ""
	}
	return aws.ToString(s.machine.Name)
}

func (s *shellSession) machineArn() string {
	if s.machine == nil {
		return ""
	}
	return aws.ToString(s.machine.StateMachineArn)
}

func (s *shellSession) prompt() string {
	if s.machine == nil {
		return "sf ❯ "
	}
	name := s.machineName()
	if len(name) > 30 {
		name = name[:27] + "..."
	}
	return fmt.Sprintf("sf [%s] ❯ ", stylePick.Render(name))
}

// ── root sf ───────────────────────────────────────────────────────────────────

var sfCmd = &cobra.Command{
	Use:   "sf",
	Short: "AWS Step Functions — lista, executa e reprocessa",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSFShell()
	},
}

// ── picker de machines (funciona em qualquer terminal) ────────────────────────

func runMachinePicker(sess *shellSession) (*types.StateMachineListItem, error) {
	machines, err := fetchAllMachines(sess.ctx, sess.client)
	if err != nil {
		return nil, err
	}
	if len(machines) == 0 {
		return nil, fmt.Errorf("nenhuma state machine encontrada")
	}

	scanner := bufio.NewScanner(os.Stdin)
	filtered := machines

	for {
		fmt.Printf("\n%s\n\n", styleHeader.Render("  STATE MACHINES"))

		for i, m := range filtered {
			fmt.Printf("  %s %s\n",
				styleNumber.Render(fmt.Sprintf("[%d]", i+1)),
				styleSuccess.Render(aws.ToString(m.Name)),
			)
		}

		fmt.Printf("\n  %s %s\n  ",
			styleDim.Render("Filtro (Enter pra listar tudo · número pra selecionar):"),
			styleDim.Render(fmt.Sprintf("%d machines", len(filtered))),
		)

		if !scanner.Scan() {
			return nil, nil
		}
		input := strings.TrimSpace(scanner.Text())

		if input == "" {
			filtered = machines
			continue
		}

		// número → seleciona
		var n int
		if _, err := fmt.Sscanf(input, "%d", &n); err == nil {
			if n >= 1 && n <= len(filtered) {
				chosen := filtered[n-1]
				return &chosen, nil
			}
			fmt.Println(styleError.Render(fmt.Sprintf("  Escolha entre 1 e %d", len(filtered))))
			continue
		}

		// texto → filtra
		f := strings.ToLower(input)
		var next []types.StateMachineListItem
		for _, m := range machines {
			if strings.Contains(strings.ToLower(aws.ToString(m.Name)), f) {
				next = append(next, m)
			}
		}
		if len(next) == 0 {
			fmt.Println(styleWarning.Render("  Nenhuma machine com esse nome."))
			filtered = machines
			continue
		}
		if len(next) == 1 {
			return &next[0], nil
		}
		filtered = next
	}
}

// ── sf list ───────────────────────────────────────────────────────────────────

var sfListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "Lista e filtra state machines",
	RunE:    runList,
}

func runList(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	client, err := awsClient(ctx)
	if err != nil {
		return err
	}
	machines, err := fetchAllMachines(ctx, client)
	if err != nil {
		return err
	}

	if jsonOutput {
		type row struct {
			Name string `json:"name"`
			ARN  string `json:"arn"`
		}
		result := make([]row, len(machines))
		for i, m := range machines {
			result[i] = row{Name: aws.ToString(m.Name), ARN: aws.ToString(m.StateMachineArn)}
		}
		b, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(b))
		return nil
	}

	fmt.Printf("\n%s\n\n", styleHeader.Render("  STATE MACHINES"))
	fmt.Printf("  %-4s %-50s %s\n", styleHeader.Render("#"), styleHeader.Render("NOME"), styleHeader.Render("ARN"))
	fmt.Println("  " + strings.Repeat("─", 115))
	for i, m := range machines {
		fmt.Printf("  %-4s %-50s %s\n",
			styleNumber.Render(fmt.Sprintf("[%d]", i+1)),
			styleSuccess.Render(aws.ToString(m.Name)),
			styleARN.Render(aws.ToString(m.StateMachineArn)),
		)
	}
	fmt.Printf("\n  %s\n\n", styleDim.Render(fmt.Sprintf("%d machines", len(machines))))
	return nil
}

// ── sf executions ─────────────────────────────────────────────────────────────

var sfExecutionsCmd = &cobra.Command{
	Use:     "executions [nome-ou-arn]",
	Aliases: []string{"ex", "exec"},
	Short:   "Lista execuções de uma state machine",
	Args:    cobra.MaximumNArgs(1),
	RunE:    runExecutions,
}

func runExecutions(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	client, err := awsClient(ctx)
	if err != nil {
		return err
	}
	var machineArn string
	if len(args) > 0 {
		machineArn, err = resolveArn(ctx, client, args[0])
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("passe o nome/ARN ou use o shell interativo")
	}
	execs, err := fetchExecutions(ctx, client, machineArn, maxResults, statusFilter)
	if err != nil {
		return err
	}

	if jsonOutput {
		type row struct {
			Name      string `json:"name"`
			ARN       string `json:"arn"`
			Status    string `json:"status"`
			StartDate string `json:"startDate"`
		}
		result := make([]row, len(execs))
		for i, e := range execs {
			result[i] = row{
				Name:      aws.ToString(e.Name),
				ARN:       aws.ToString(e.ExecutionArn),
				Status:    string(e.Status),
				StartDate: e.StartDate.Format(time.RFC3339),
			}
		}
		b, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(b))
		return nil
	}

	printExecutionsTable(execs)
	return nil
}

func printExecutionsTable(execs []types.ExecutionListItem) {
	fmt.Printf("\n%s\n\n", styleHeader.Render("  EXECUÇÕES"))
	fmt.Printf("  %-4s %-22s %-12s %s\n",
		styleHeader.Render("#"),
		styleHeader.Render("INÍCIO"),
		styleHeader.Render("STATUS"),
		styleHeader.Render("NOME"),
	)
	fmt.Println("  " + strings.Repeat("─", 90))
	for i, e := range execs {
		fmt.Printf("  %-4s %-22s %-12s %s\n",
			styleNumber.Render(fmt.Sprintf("[%d]", i+1)),
			e.StartDate.Format("2006-01-02 15:04:05"),
			styleStatusExec(string(e.Status)),
			styleDim.Render(aws.ToString(e.Name)),
		)
	}
	fmt.Println()
}

// ── sf history ────────────────────────────────────────────────────────────────

var sfHistoryCmd = &cobra.Command{
	Use:     "history <execution-arn>",
	Aliases: []string{"h", "hist"},
	Short:   "Mostra steps de uma execução",
	Args:    cobra.ExactArgs(1),
	RunE:    runHistory,
}

func runHistory(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	client, err := awsClient(ctx)
	if err != nil {
		return err
	}
	out, err := client.GetExecutionHistory(ctx, &sfn.GetExecutionHistoryInput{
		ExecutionArn:         aws.String(args[0]),
		IncludeExecutionData: aws.Bool(true),
	})
	if err != nil {
		return fmt.Errorf("erro ao buscar histórico: %w", err)
	}
	fmt.Printf("\n%s\n\n", styleHeader.Render("  HISTÓRICO"))
	for _, e := range out.Events {
		ts := e.Timestamp.Format("15:04:05")
		icon, styled := iconAndStyle(e.Type, string(e.Type))
		detail := detailSummary(e)
		line := fmt.Sprintf("  %s [%s] %s", icon, styleDim.Render(ts), styled)
		if detail != "" {
			line += "  " + styleDim.Render(detail)
		}
		fmt.Println(line)
	}
	fmt.Println()
	return nil
}

// ── sf input ──────────────────────────────────────────────────────────────────

var sfInputCmd = &cobra.Command{
	Use:     "input <execution-arn>",
	Aliases: []string{"in"},
	Short:   "Mostra input/output de uma execução formatado",
	Args:    cobra.ExactArgs(1),
	RunE:    runInput,
}

func runInput(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	client, err := awsClient(ctx)
	if err != nil {
		return err
	}
	desc, err := client.DescribeExecution(ctx, &sfn.DescribeExecutionInput{
		ExecutionArn: aws.String(args[0]),
	})
	if err != nil {
		return fmt.Errorf("erro ao descrever execução: %w", err)
	}
	fmt.Printf("\n%s\n  Status: %s\n\n%s\n%s\n",
		styleHeader.Render("  EXECUÇÃO"),
		styleStatusExec(string(desc.Status)),
		styleHeader.Render("  INPUT"),
		prettyJSON(aws.ToString(desc.Input)),
	)
	if desc.Output != nil {
		fmt.Printf("\n%s\n%s\n\n",
			styleHeader.Render("  OUTPUT"),
			prettyJSON(aws.ToString(desc.Output)),
		)
	}
	return nil
}

// ── sf start ──────────────────────────────────────────────────────────────────

var sfStartCmd = &cobra.Command{
	Use:     "start <machine-arn-ou-nome>",
	Aliases: []string{"st", "run"},
	Short:   "Inicia nova execução",
	Args:    cobra.ExactArgs(1),
	RunE:    runStart,
}

func runStart(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	client, err := awsClient(ctx)
	if err != nil {
		return err
	}
	machineArn, err := resolveArn(ctx, client, args[0])
	if err != nil {
		return err
	}
	if inputJSON == "" {
		inputJSON = "{}"
	}
	if !noConfirm {
		fmt.Printf("\n  Iniciar execução em %s?\n  Input: %s\n", styleSuccess.Render(args[0]), styleDim.Render(inputJSON))
		if !promptConfirm("Confirmar") {
			fmt.Println(styleDim.Render("  Cancelado."))
			return nil
		}
	}
	execName := fmt.Sprintf("moc-%d", time.Now().Unix())
	out, err := client.StartExecution(ctx, &sfn.StartExecutionInput{
		StateMachineArn: aws.String(machineArn),
		Input:           aws.String(inputJSON),
		Name:            aws.String(execName),
	})
	if err != nil {
		return fmt.Errorf("erro ao iniciar execução: %w", err)
	}
	fmt.Printf("\n%s\n  ARN: %s\n  Início: %s\n\n",
		styleSuccess.Render("✓ Execução iniciada!"),
		styleARN.Render(aws.ToString(out.ExecutionArn)),
		out.StartDate.Format("2006-01-02 15:04:05"),
	)
	return nil
}

// ── sf rerun ──────────────────────────────────────────────────────────────────

var sfRerunCmd = &cobra.Command{
	Use:     "rerun <execution-arn>",
	Aliases: []string{"rr", "retry"},
	Short:   "Reprocessa execução com mesmo input",
	Args:    cobra.ExactArgs(1),
	RunE:    runRerun,
}

func runRerun(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	client, err := awsClient(ctx)
	if err != nil {
		return err
	}
	desc, err := client.DescribeExecution(ctx, &sfn.DescribeExecutionInput{
		ExecutionArn: aws.String(args[0]),
	})
	if err != nil {
		return fmt.Errorf("erro ao descrever execução: %w", err)
	}
	originalInput := aws.ToString(desc.Input)
	machineArn := aws.ToString(desc.StateMachineArn)

	fmt.Printf("\n  Status: %s\n  Input: %s\n\n",
		styleStatusExec(string(desc.Status)),
		styleDim.Render(originalInput),
	)
	if !noConfirm {
		if !promptConfirm("Reprocessar com mesmo input") {
			fmt.Println(styleDim.Render("  Cancelado."))
			return nil
		}
	}
	out, err := client.StartExecution(ctx, &sfn.StartExecutionInput{
		StateMachineArn: aws.String(machineArn),
		Input:           aws.String(originalInput),
		Name:            aws.String(fmt.Sprintf("rerun-%d", time.Now().Unix())),
	})
	if err != nil {
		return fmt.Errorf("erro ao reiniciar: %w", err)
	}
	fmt.Printf("%s\n  ARN: %s\n\n",
		styleSuccess.Render("✓ Reprocessamento iniciado!"),
		styleARN.Render(aws.ToString(out.ExecutionArn)),
	)
	return nil
}

// ── sf status ─────────────────────────────────────────────────────────────────

var sfStatusCmd = &cobra.Command{
	Use:     "status [nome-ou-arn]",
	Aliases: []string{"s"},
	Short:   "Resumo rápido das últimas execuções",
	Args:    cobra.MaximumNArgs(1),
	RunE:    runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	client, err := awsClient(ctx)
	if err != nil {
		return err
	}
	var arn, name string
	if len(args) > 0 {
		arn, err = resolveArn(ctx, client, args[0])
		name = args[0]
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("passe o nome/ARN ou use o shell interativo")
	}
	execs, err := fetchExecutions(ctx, client, arn, 5, "")
	if err != nil {
		return err
	}
	running, failed, succeeded := 0, 0, 0
	for _, e := range execs {
		switch e.Status {
		case types.ExecutionStatusRunning:
			running++
		case types.ExecutionStatusFailed, types.ExecutionStatusTimedOut, types.ExecutionStatusAborted:
			failed++
		case types.ExecutionStatusSucceeded:
			succeeded++
		}
	}
	fmt.Printf("\n%s\n\n  %s %s   %s %s   %s %s\n\n",
		styleHeader.Render("  STATUS — "+name),
		styleRunning.Render("●"), styleRunning.Render(fmt.Sprintf("%d running", running)),
		styleSuccess.Render("●"), styleSuccess.Render(fmt.Sprintf("%d succeeded", succeeded)),
		styleError.Render("●"), styleError.Render(fmt.Sprintf("%d failed", failed)),
	)
	for _, e := range execs {
		fmt.Printf("  %s  %s  %s\n",
			styleStatusExec(string(e.Status)),
			styleDim.Render(e.StartDate.Format("01/02 15:04:05")),
			styleDim.Render(aws.ToString(e.Name)),
		)
	}
	fmt.Println()
	return nil
}

// ── sf watch ──────────────────────────────────────────────────────────────────

var sfWatchCmd = &cobra.Command{
	Use:     "watch [intervalo-segundos]",
	Aliases: []string{"w", "dash", "d"},
	Short:   "Dashboard live de execuções RUNNING",
	Args:    cobra.MaximumNArgs(1),
	RunE:    runWatch,
}

func runWatch(cmd *cobra.Command, args []string) error {
	return runWatchWithContext(args, "", "")
}

func runWatchWithContext(args []string, filterArn, filterName string) error {
	ctx := context.Background()
	client, err := awsClient(ctx)
	if err != nil {
		return err
	}
	interval := watchSecs
	if len(args) > 0 {
		var n int
		if _, err := fmt.Sscanf(args[0], "%d", &n); err == nil && n > 0 {
			interval = n
		}
	}
	return runWatchTUI(client, ctx, interval, filterArn, filterName)
}

// ── sf tail ───────────────────────────────────────────────────────────────────

var sfTailCmd = &cobra.Command{
	Use:     "tail <execution-arn>",
	Aliases: []string{"t", "follow", "f"},
	Short:   "Acompanha execução em tempo real",
	Args:    cobra.ExactArgs(1),
	RunE:    runTail,
}

func runTail(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	client, err := awsClient(ctx)
	if err != nil {
		return err
	}
	execArn := args[0]
	fmt.Printf("\n%s %s\n\n  %s\n\n",
		styleHeader.Render("  TAIL —"),
		styleARN.Render(execArn),
		styleDim.Render("Aguardando eventos... Ctrl+C para sair"),
	)
	seen := map[int64]bool{}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-sig:
			fmt.Printf("\n  %s\n\n", styleDim.Render("Tail encerrado."))
			return nil
		default:
		}
		out, err := client.GetExecutionHistory(ctx, &sfn.GetExecutionHistoryInput{
			ExecutionArn:         aws.String(execArn),
			IncludeExecutionData: aws.Bool(true),
		})
		if err != nil {
			time.Sleep(3 * time.Second)
			continue
		}
		for _, e := range out.Events {
			if seen[e.Id] {
				continue
			}
			seen[e.Id] = true
			ts := e.Timestamp.Format("15:04:05")
			icon, styled := iconAndStyle(e.Type, string(e.Type))
			detail := detailSummary(e)
			line := fmt.Sprintf("  %s [%s] %s", icon, styleDim.Render(ts), styled)
			if detail != "" {
				line += "  " + styleDim.Render(detail)
			}
			fmt.Println(line)
		}
		desc, err := client.DescribeExecution(ctx, &sfn.DescribeExecutionInput{ExecutionArn: aws.String(execArn)})
		if err == nil && desc.Status != types.ExecutionStatusRunning {
			fmt.Printf("\n  %s %s\n\n",
				styleHeader.Render("Finalizada:"),
				styleStatusExec(string(desc.Status)),
			)
			return nil
		}
		time.Sleep(2 * time.Second)
	}
}

// ── shell interativo (bubbletea TUI) ──────────────────────────────────────────

func runSFShell() error {
	ctx := context.Background()
	client, err := awsClient(ctx)
	if err != nil {
		return err
	}
	return runSFShellTUI(client, ctx)
}

// sessArg retorna ARN da machine: prioriza sargs[0], fallback pra sess.machine
func sessArg(sess *shellSession, sargs []string, cmd string) (arn, name string, ok bool) {
	if len(sargs) > 0 {
		var err error
		arn, err = resolveArn(sess.ctx, sess.client, sargs[0])
		if err != nil {
			fmt.Println(styleError.Render("  " + err.Error()))
			return "", "", false
		}
		return arn, sargs[0], true
	}
	if sess.machine != nil {
		return sess.machineArn(), sess.machineName(), true
	}
	fmt.Printf("  %s\n  %s\n",
		styleWarning.Render("Nenhuma machine selecionada."),
		styleDim.Render("Use 'ls' para selecionar ou passe o nome: "+cmd+" <nome>"),
	)
	return "", "", false
}

// resolvePartial resolve nome parcial → machine
func resolvePartial(sess *shellSession, partial string) (*types.StateMachineListItem, error) {
	machines, err := fetchAllMachines(sess.ctx, sess.client)
	if err != nil {
		return nil, err
	}
	f := strings.ToLower(partial)
	var matches []types.StateMachineListItem
	for _, m := range machines {
		if strings.Contains(strings.ToLower(aws.ToString(m.Name)), f) {
			matches = append(matches, m)
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("nenhuma machine encontrada com '%s'", partial)
	}
	if len(matches) == 1 {
		return &matches[0], nil
	}
	// múltiplos → mostra lista pra escolher
	fmt.Printf("\n  %s\n\n", styleDim.Render("Múltiplas machines encontradas:"))
	for i, m := range matches {
		fmt.Printf("  %s %s\n", styleNumber.Render(fmt.Sprintf("[%d]", i+1)), styleSuccess.Render(aws.ToString(m.Name)))
	}
	n := promptInt("Escolha", 1, len(matches))
	if n < 0 {
		return nil, nil
	}
	return &matches[n-1], nil
}

// execSessionPick lista execuções e deixa escolher uma para tail/rerun/history
func execSessionPick(sess *shellSession, machineArn, machineName string, scanner *bufio.Scanner) {
	execs, err := fetchExecutions(sess.ctx, sess.client, machineArn, maxResults, statusFilter)
	if err != nil {
		fmt.Println(styleError.Render("  Erro: " + err.Error()))
		return
	}
	if len(execs) == 0 {
		fmt.Println(styleWarning.Render("  Nenhuma execução encontrada."))
		return
	}

	fmt.Printf("\n%s\n\n", styleHeader.Render("  EXECUÇÕES — "+machineName))
	for i, e := range execs {
		fmt.Printf("  %s %-12s %s\n",
			styleNumber.Render(fmt.Sprintf("[%d]", i+1)),
			styleStatusExec(string(e.Status)),
			styleDim.Render(e.StartDate.Format("2006-01-02 15:04:05")),
		)
	}

	fmt.Printf("\n  %s\n  Ação: ", stylePick.Render("[número] escolhe  ·  sufixo: h=history  r=rerun  t=tail  i=input"))
	if !scanner.Scan() {
		return
	}
	input := strings.TrimSpace(scanner.Text())
	if input == "" {
		return
	}

	// detecta ação no sufixo: "1r", "2t", "3h"
	action := ""
	numStr := input
	if len(input) > 0 {
		last := string(input[len(input)-1])
		if last == "r" || last == "t" || last == "h" || last == "i" {
			action = last
			numStr = input[:len(input)-1]
		}
	}

	var n int
	if _, err := fmt.Sscanf(numStr, "%d", &n); err != nil || n < 1 || n > len(execs) {
		fmt.Println(styleError.Render(fmt.Sprintf("  Número inválido (1-%d)", len(execs))))
		return
	}

	execArn := aws.ToString(execs[n-1].ExecutionArn)
	if action == "" {
		// pede ação separada
		fmt.Printf("  %s\n  Ação: ", stylePick.Render("[t] tail  [r] rerun  [h] history  [i] input"))
		if !scanner.Scan() {
			return
		}
		action = strings.TrimSpace(scanner.Text())
	}

	switch action {
	case "t", "tail":
		runTail(nil, []string{execArn})
	case "r", "rerun":
		runRerun(nil, []string{execArn})
	case "h", "history":
		runHistory(nil, []string{execArn})
	case "i", "input":
		runInput(nil, []string{execArn})
	default:
		fmt.Println(styleDim.Render("  Cancelado."))
	}
}

func runStatusDirect(ctx context.Context, client *sfn.Client, arn, name string) {
	execs, err := fetchExecutions(ctx, client, arn, 5, "")
	if err != nil {
		fmt.Println(styleError.Render("  Erro: " + err.Error()))
		return
	}
	running, failed, succeeded := 0, 0, 0
	for _, e := range execs {
		switch e.Status {
		case types.ExecutionStatusRunning:
			running++
		case types.ExecutionStatusFailed, types.ExecutionStatusTimedOut, types.ExecutionStatusAborted:
			failed++
		case types.ExecutionStatusSucceeded:
			succeeded++
		}
	}
	fmt.Printf("\n%s\n\n  %s %s   %s %s   %s %s\n\n",
		styleHeader.Render("  STATUS — "+name),
		styleRunning.Render("●"), styleRunning.Render(fmt.Sprintf("%d running", running)),
		styleSuccess.Render("●"), styleSuccess.Render(fmt.Sprintf("%d succeeded", succeeded)),
		styleError.Render("●"), styleError.Render(fmt.Sprintf("%d failed", failed)),
	)
	for _, e := range execs {
		fmt.Printf("  %s  %s  %s\n",
			styleStatusExec(string(e.Status)),
			styleDim.Render(e.StartDate.Format("01/02 15:04:05")),
			styleDim.Render(aws.ToString(e.Name)),
		)
	}
	fmt.Println()
}

func printSFHelp() {
	cmds := [][]string{
		{"ls / list", "Lista machines e seleciona contexto"},
		{"use <parte-do-nome>", "Seleciona machine por nome parcial"},
		{"clear / unuse", "Remove machine selecionada"},
		{"", ""},
		{"executions / ex [nome]", "Lista execuções (usa machine do contexto)"},
		{"status / s [nome]", "Resumo rápido (usa contexto)"},
		{"watch / w [N]", "Dashboard live (filtra por contexto se selecionado)"},
		{"tail / t [arn]", "Tail — sem ARN, abre picker de execuções"},
		{"", ""},
		{"history / h <arn>", "Steps de uma execução"},
		{"input / in <arn>", "Input/output formatado"},
		{"start / st <nome>", "Inicia execução"},
		{"rerun / rr <arn>", "Reprocessa com mesmo input"},
		{"exit / q", "Sai"},
	}
	fmt.Println()
	for _, c := range cmds {
		if c[0] == "" {
			fmt.Println()
			continue
		}
		fmt.Printf("  %-30s %s\n", styleSuccess.Render(c[0]), styleDim.Render(c[1]))
	}
	fmt.Println()
}

// ── init ──────────────────────────────────────────────────────────────────────

func init() {
	sfCmd.PersistentFlags().StringVarP(&region, "region", "r", "", "AWS region (padrão: config ou us-east-1)")
	sfCmd.PersistentFlags().StringVarP(&profile, "profile", "p", "", "AWS profile")
	viper.BindPFlag("region", sfCmd.PersistentFlags().Lookup("region"))
	viper.BindPFlag("profile", sfCmd.PersistentFlags().Lookup("profile"))

	sfExecutionsCmd.Flags().Int32VarP(&maxResults, "max", "n", 10, "Máximo de execuções")
	sfExecutionsCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output em JSON")
	sfExecutionsCmd.Flags().StringVarP(&statusFilter, "status", "s", "", "Filtrar: RUNNING|SUCCEEDED|FAILED")

	sfListCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output em JSON")
	sfWatchCmd.Flags().IntVarP(&watchSecs, "interval", "i", 5, "Intervalo de refresh em segundos")
	sfStartCmd.Flags().StringVarP(&inputJSON, "input", "i", "", "JSON de input")
	sfStartCmd.Flags().BoolVar(&noConfirm, "yes", false, "Pular confirmação")
	sfRerunCmd.Flags().BoolVar(&noConfirm, "yes", false, "Pular confirmação")

	sfCmd.AddCommand(sfListCmd)
	sfCmd.AddCommand(sfExecutionsCmd)
	sfCmd.AddCommand(sfHistoryCmd)
	sfCmd.AddCommand(sfInputCmd)
	sfCmd.AddCommand(sfStartCmd)
	sfCmd.AddCommand(sfRerunCmd)
	sfCmd.AddCommand(sfStatusCmd)
	sfCmd.AddCommand(sfWatchCmd)
	sfCmd.AddCommand(sfTailCmd)
}
