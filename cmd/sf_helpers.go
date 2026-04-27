package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sfn"
	"github.com/aws/aws-sdk-go-v2/service/sfn/types"
	"github.com/spf13/viper"
)

// ── cliente AWS ───────────────────────────────────────────────────────────────

func awsClient(ctx context.Context) (*sfn.Client, error) {
	r := viper.GetString("region")
	p := viper.GetString("profile")
	// flag overrides viper
	if region != "" {
		r = region
	}
	if profile != "" {
		p = profile
	}

	opts := []func(*config.LoadOptions) error{
		config.WithRegion(r),
	}
	if p != "" {
		opts = append(opts, config.WithSharedConfigProfile(p))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("erro ao carregar config AWS: %w", err)
	}
	return sfn.NewFromConfig(cfg), nil
}

// ── cache de machines ─────────────────────────────────────────────────────────

var (
	cacheMu       sync.Mutex
	cacheEntries  []types.StateMachineListItem
	cacheTime     time.Time
	cacheRegKey   string
)

const cacheTTL = 5 * time.Minute

func fetchAllMachines(ctx context.Context, client *sfn.Client) ([]types.StateMachineListItem, error) {
	key := viper.GetString("region") + "|" + viper.GetString("profile") + "|" + region + "|" + profile

	cacheMu.Lock()
	defer cacheMu.Unlock()

	if time.Since(cacheTime) < cacheTTL && cacheRegKey == key && len(cacheEntries) > 0 {
		return cacheEntries, nil
	}

	var all []types.StateMachineListItem
	var nextToken *string
	for {
		out, err := client.ListStateMachines(ctx, &sfn.ListStateMachinesInput{NextToken: nextToken})
		if err != nil {
			return nil, err
		}
		all = append(all, out.StateMachines...)
		if out.NextToken == nil {
			break
		}
		nextToken = out.NextToken
	}

	cacheEntries = all
	cacheTime = time.Now()
	cacheRegKey = key
	return all, nil
}

func invalidateCache() {
	cacheMu.Lock()
	cacheTime = time.Time{}
	cacheMu.Unlock()
}

// ── fetch executions ──────────────────────────────────────────────────────────

func fetchExecutions(ctx context.Context, client *sfn.Client, machineArn string, max int32, statusFilter string) ([]types.ExecutionListItem, error) {
	input := &sfn.ListExecutionsInput{
		StateMachineArn: aws.String(machineArn),
		MaxResults:      max,
	}
	if statusFilter != "" {
		input.StatusFilter = types.ExecutionStatus(statusFilter)
	}
	out, err := client.ListExecutions(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("erro ao listar execuções: %w", err)
	}
	return out.Executions, nil
}

// ── resolve nome → ARN ────────────────────────────────────────────────────────

func resolveArn(ctx context.Context, client *sfn.Client, nameOrArn string) (string, error) {
	if strings.HasPrefix(nameOrArn, "arn:") {
		return nameOrArn, nil
	}
	machines, err := fetchAllMachines(ctx, client)
	if err != nil {
		return "", err
	}
	for _, m := range machines {
		if aws.ToString(m.Name) == nameOrArn {
			return aws.ToString(m.StateMachineArn), nil
		}
	}
	return "", fmt.Errorf("state machine não encontrada: %s", nameOrArn)
}

// ── formatação ────────────────────────────────────────────────────────────────

func prettyJSON(s string) string {
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

func styleStatusExec(s string) string {
	switch s {
	case "RUNNING":
		return styleRunning.Render(s)
	case "SUCCEEDED":
		return styleSuccess.Render(s)
	case "FAILED", "TIMED_OUT", "ABORTED":
		return styleError.Render(s)
	default:
		return styleDim.Render(s)
	}
}

func iconAndStyle(t types.HistoryEventType, label string) (string, string) {
	switch t {
	case types.HistoryEventTypeExecutionStarted:
		return "▶", styleWarning.Render(label)
	case types.HistoryEventTypeExecutionSucceeded:
		return "✓", styleSuccess.Render(label)
	case types.HistoryEventTypeExecutionFailed, types.HistoryEventTypeExecutionTimedOut:
		return "✗", styleError.Render(label)
	case types.HistoryEventTypeExecutionAborted:
		return "⊘", styleError.Render(label)
	default:
		if strings.HasSuffix(string(t), "StateEntered") {
			return "→", styleWarning.Render(label)
		}
		if strings.HasSuffix(string(t), "StateExited") {
			return "←", styleSuccess.Render(label)
		}
		return "·", styleDim.Render(label)
	}
}

func detailSummary(e types.HistoryEvent) string {
	switch {
	case e.ExecutionFailedEventDetails != nil:
		d := e.ExecutionFailedEventDetails
		return fmt.Sprintf("ERRO: %s — %s", aws.ToString(d.Error), aws.ToString(d.Cause))
	case e.StateEnteredEventDetails != nil:
		return aws.ToString(e.StateEnteredEventDetails.Name)
	case e.StateExitedEventDetails != nil:
		out := aws.ToString(e.StateExitedEventDetails.Output)
		if len(out) > 100 {
			out = out[:100] + "..."
		}
		return out
	case e.LambdaFunctionFailedEventDetails != nil:
		d := e.LambdaFunctionFailedEventDetails
		return fmt.Sprintf("Lambda ERRO: %s", aws.ToString(d.Error))
	default:
		return ""
	}
}

// ── prompt numérico ───────────────────────────────────────────────────────────

func promptConfirm(question string) bool {
	fmt.Printf("  %s [y/N]: ", stylePick.Render(question))
	var ans string
	fmt.Scanln(&ans)
	return strings.ToLower(strings.TrimSpace(ans)) == "y"
}

func promptInt(label string, min, max int) int {
	fmt.Printf("  %s [%d-%d]: ", stylePick.Render(label), min, max)
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return -1
	}
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(scanner.Text()), "%d", &n); err != nil || n < min || n > max {
		fmt.Println(styleError.Render(fmt.Sprintf("  Escolha entre %d e %d", min, max)))
		return -1
	}
	return n
}
