package cmd

import (
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

// ── root sf ───────────────────────────────────────────────────────────────────

var sfCmd = &cobra.Command{
	Use:   "sf",
	Short: "AWS Step Functions — list, run, re-run",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSFShell()
	},
}

// ── sf list ───────────────────────────────────────────────────────────────────

var sfListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List and filter state machines",
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
	fmt.Printf("  %-4s %-50s %s\n", styleHeader.Render("#"), styleHeader.Render("NAME"), styleHeader.Render("ARN"))
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
	Use:     "executions [name-or-arn]",
	Aliases: []string{"ex", "exec"},
	Short:   "List executions of a state machine",
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
		return fmt.Errorf("pass the name/ARN or use the interactive shell")
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
	fmt.Printf("\n%s\n\n", styleHeader.Render("  EXECUTIONS"))
	fmt.Printf("  %-4s %-22s %-12s %s\n",
		styleHeader.Render("#"),
		styleHeader.Render("STARTED"),
		styleHeader.Render("STATUS"),
		styleHeader.Render("NAME"),
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
	Short:   "Show steps of an execution",
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
		return fmt.Errorf("error fetching history: %w", err)
	}
	fmt.Printf("\n%s\n\n", styleHeader.Render("  HISTORY"))
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
	Short:   "Show formatted input/output of an execution",
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
		return fmt.Errorf("error describing execution: %w", err)
	}
	fmt.Printf("\n%s\n  Status: %s\n\n%s\n%s\n",
		styleHeader.Render("  EXECUTION"),
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
	Use:     "start <machine-arn-or-name>",
	Aliases: []string{"st", "run"},
	Short:   "Start a new execution",
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
		fmt.Printf("\n  Start execution on %s?\n  Input: %s\n", styleSuccess.Render(args[0]), styleDim.Render(inputJSON))
		if !promptConfirm("Confirm") {
			fmt.Println(styleDim.Render("  Cancelled."))
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
		return fmt.Errorf("error starting execution: %w", err)
	}
	fmt.Printf("\n%s\n  ARN: %s\n  Started: %s\n\n",
		styleSuccess.Render("✓ Execution started!"),
		styleARN.Render(aws.ToString(out.ExecutionArn)),
		out.StartDate.Format("2006-01-02 15:04:05"),
	)
	return nil
}

// ── sf rerun ──────────────────────────────────────────────────────────────────

var sfRerunCmd = &cobra.Command{
	Use:     "rerun <execution-arn>",
	Aliases: []string{"rr", "retry"},
	Short:   "Re-run execution with same input",
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
		return fmt.Errorf("error describing execution: %w", err)
	}
	originalInput := aws.ToString(desc.Input)
	machineArn := aws.ToString(desc.StateMachineArn)

	fmt.Printf("\n  Status: %s\n  Input: %s\n\n",
		styleStatusExec(string(desc.Status)),
		styleDim.Render(originalInput),
	)
	if !noConfirm {
		if !promptConfirm("Re-run with same input") {
			fmt.Println(styleDim.Render("  Cancelled."))
			return nil
		}
	}
	out, err := client.StartExecution(ctx, &sfn.StartExecutionInput{
		StateMachineArn: aws.String(machineArn),
		Input:           aws.String(originalInput),
		Name:            aws.String(fmt.Sprintf("rerun-%d", time.Now().Unix())),
	})
	if err != nil {
		return fmt.Errorf("error re-running: %w", err)
	}
	fmt.Printf("%s\n  ARN: %s\n\n",
		styleSuccess.Render("✓ Re-run started!"),
		styleARN.Render(aws.ToString(out.ExecutionArn)),
	)
	return nil
}

// ── sf status ─────────────────────────────────────────────────────────────────

var sfStatusCmd = &cobra.Command{
	Use:     "status [name-or-arn]",
	Aliases: []string{"s"},
	Short:   "Quick summary of latest executions",
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
		return fmt.Errorf("pass the name/ARN or use the interactive shell")
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
	Use:     "watch [interval-seconds]",
	Aliases: []string{"w", "dash", "d"},
	Short:   "Live dashboard of RUNNING executions",
	Args:    cobra.MaximumNArgs(1),
	RunE:    runWatch,
}

func runWatch(cmd *cobra.Command, args []string) error {
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
	return runWatchTUI(client, ctx, interval, "", "")
}

// ── sf tail ───────────────────────────────────────────────────────────────────

var sfTailCmd = &cobra.Command{
	Use:     "tail <execution-arn>",
	Aliases: []string{"t", "follow", "f"},
	Short:   "Follow an execution in real time",
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
		styleDim.Render("Waiting for events... Ctrl+C to exit"),
	)
	seen := map[int64]bool{}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-sig:
			fmt.Printf("\n  %s\n\n", styleDim.Render("Tail stopped."))
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
				styleHeader.Render("Finished:"),
				styleStatusExec(string(desc.Status)),
			)
			return nil
		}
		time.Sleep(2 * time.Second)
	}
}

// ── interactive shell (bubbletea TUI) ─────────────────────────────────────────

func runSFShell() error {
	ctx := context.Background()
	client, err := awsClient(ctx)
	if err != nil {
		return err
	}
	return runSFShellTUI(client, ctx)
}

// ── init ──────────────────────────────────────────────────────────────────────

func init() {
	sfCmd.PersistentFlags().StringVarP(&region, "region", "r", "", "AWS region (default: config or us-east-1)")
	sfCmd.PersistentFlags().StringVarP(&profile, "profile", "p", "", "AWS profile")
	viper.BindPFlag("region", sfCmd.PersistentFlags().Lookup("region"))
	viper.BindPFlag("profile", sfCmd.PersistentFlags().Lookup("profile"))

	sfExecutionsCmd.Flags().Int32VarP(&maxResults, "max", "n", 10, "Max number of executions")
	sfExecutionsCmd.Flags().BoolVar(&jsonOutput, "json", false, "JSON output")
	sfExecutionsCmd.Flags().StringVarP(&statusFilter, "status", "s", "", "Filter: RUNNING|SUCCEEDED|FAILED")

	sfListCmd.Flags().BoolVar(&jsonOutput, "json", false, "JSON output")
	sfWatchCmd.Flags().IntVarP(&watchSecs, "interval", "i", 5, "Refresh interval in seconds")
	sfStartCmd.Flags().StringVarP(&inputJSON, "input", "i", "", "JSON input")
	sfStartCmd.Flags().BoolVar(&noConfirm, "yes", false, "Skip confirmation")
	sfRerunCmd.Flags().BoolVar(&noConfirm, "yes", false, "Skip confirmation")

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
