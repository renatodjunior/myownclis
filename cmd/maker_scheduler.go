package cmd

import (
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// Notification is sent to NotifyCh when a scheduled command completes.
type Notification struct {
	Name   string
	Output string
	Status string
	RunAt  time.Time
}

var (
	NotifyCh      = make(chan Notification, 20)
	inSessionCron *cron.Cron
)

// StartInSessionScheduler reads all commands/chains with in_session=true and
// starts goroutines that fire on their cron schedules.
func StartInSessionScheduler() {
	inSessionCron = cron.New()

	cmdlets, _ := ListCmdlets()
	for _, cmdlet := range cmdlets {
		cmds, _ := ListCommands(cmdlet)
		for _, c := range cmds {
			if !c.Schedule.InSession || c.Schedule.Cron == "" {
				continue
			}
			inSessionCron.AddFunc(c.Schedule.Cron, func() { //nolint:errcheck
				name := logName(c.Cmdlet, CommandSlug(c.Cmdlet, c.Command))
				err := RunCommand(c, false)
				status := "success"
				if err != nil {
					status = "failed"
				}
				lines, _ := TailLog(name, 5)
				select {
				case NotifyCh <- Notification{
					Name:   c.Cmdlet + " " + c.Command,
					Output: strings.Join(lines, "\n"),
					Status: status,
					RunAt:  time.Now(),
				}:
				default:
				}
			})
		}
	}

	chains, _ := ListChains()
	for _, ch := range chains {
		if !ch.Schedule.InSession || ch.Schedule.Cron == "" {
			continue
		}
		inSessionCron.AddFunc(ch.Schedule.Cron, func() { //nolint:errcheck
			err := RunChain(ch, false)
			status := "success"
			if err != nil {
				status = "failed"
			}
			lines, _ := TailLog("chain-"+ch.Name, 5)
			select {
			case NotifyCh <- Notification{
				Name:   "chain:" + ch.Name,
				Output: strings.Join(lines, "\n"),
				Status: status,
				RunAt:  time.Now(),
			}:
			default:
			}
		})
	}

	inSessionCron.Start()
}

func StopInSessionScheduler() {
	if inSessionCron != nil {
		inSessionCron.Stop()
	}
}

// ValidateCron parses the cron expression and returns an error if invalid.
func ValidateCron(expr string) error {
	p := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	_, err := p.Parse(expr)
	return err
}

// ExpandCronAlias translates friendly expressions to standard 5-field cron.
// Returns the input unchanged when no alias matched, so explicit "* * * * *"
// expressions and robfig "@daily"-style shortcuts continue to work as-is.
//
// Supported aliases (case-insensitive, whitespace-tolerant):
//
//	every minute / minutely             → * * * * *
//	every N min[s] / Nm                 → */N * * * *
//	every hour / hourly                 → 0 * * * *
//	every N hour[s] / Nh                → 0 */N * * *
//	daily / every day                   → 0 0 * * *
//	daily at H[am|pm] / daily H:MM      → MM H * * *
//	weekdays [at H[am|pm]]              → 0 9 * * 1-5  (default 9am)
//	weekends [at H[am|pm]]              → 0 9 * * 0,6  (default 9am)
func ExpandCronAlias(s string) string {
	raw := strings.TrimSpace(strings.ToLower(s))
	if raw == "" {
		return s
	}

	switch raw {
	case "every minute", "minutely":
		return "* * * * *"
	case "every hour", "hourly":
		return "0 * * * *"
	case "every day", "daily":
		return "0 0 * * *"
	case "weekdays":
		return "0 9 * * 1-5"
	case "weekends":
		return "0 9 * * 0,6"
	}

	if m := reEveryNMin.FindStringSubmatch(raw); m != nil {
		return fmt.Sprintf("*/%s * * * *", m[1])
	}
	if m := reShortMin.FindStringSubmatch(raw); m != nil {
		return fmt.Sprintf("*/%s * * * *", m[1])
	}
	if m := reEveryNHour.FindStringSubmatch(raw); m != nil {
		return fmt.Sprintf("0 */%s * * *", m[1])
	}
	if m := reShortHour.FindStringSubmatch(raw); m != nil {
		return fmt.Sprintf("0 */%s * * *", m[1])
	}
	if m := reDailyAt.FindStringSubmatch(raw); m != nil {
		h, mn := parseHourMinAmPm(m[1], m[2], m[3])
		return fmt.Sprintf("%d %d * * *", mn, h)
	}
	if m := reWeekdaysAt.FindStringSubmatch(raw); m != nil {
		h, mn := parseHourMinAmPm(m[1], m[2], m[3])
		return fmt.Sprintf("%d %d * * 1-5", mn, h)
	}
	if m := reWeekendsAt.FindStringSubmatch(raw); m != nil {
		h, mn := parseHourMinAmPm(m[1], m[2], m[3])
		return fmt.Sprintf("%d %d * * 0,6", mn, h)
	}

	return s
}

var (
	reEveryNMin  = regexp.MustCompile(`^every\s+(\d+)\s*(?:m|min|mins|minute|minutes)$`)
	reShortMin   = regexp.MustCompile(`^(\d+)m$`)
	reEveryNHour = regexp.MustCompile(`^every\s+(\d+)\s*(?:h|hour|hours)$`)
	reShortHour  = regexp.MustCompile(`^(\d+)h$`)
	reDailyAt    = regexp.MustCompile(`^(?:daily|every\s+day)\s+(?:at\s+)?(\d{1,2})(?::(\d{2}))?(am|pm)?$`)
	reWeekdaysAt = regexp.MustCompile(`^weekdays\s+(?:at\s+)?(\d{1,2})(?::(\d{2}))?(am|pm)?$`)
	reWeekendsAt = regexp.MustCompile(`^weekends\s+(?:at\s+)?(\d{1,2})(?::(\d{2}))?(am|pm)?$`)
)

func parseHourMinAmPm(hStr, mStr, ampm string) (h, mn int) {
	h, _ = strconv.Atoi(hStr)
	if mStr != "" {
		mn, _ = strconv.Atoi(mStr)
	}
	if ampm == "pm" && h < 12 {
		h += 12
	}
	if ampm == "am" && h == 12 {
		h = 0
	}
	return
}

func RegisterOSSchedule(cmdlet, slug, cronExpr string) error {
	taskName := "moc-" + cmdlet + "-" + slug
	runCmd := fmt.Sprintf("moc maker run %s %s", cmdlet, slug)
	if runtime.GOOS == "windows" {
		return registerWindowsTask(taskName, runCmd, cronExpr)
	}
	return registerCrontab(taskName, runCmd, cronExpr)
}

func UnregisterOSSchedule(cmdlet, slug string) error {
	taskName := "moc-" + cmdlet + "-" + slug
	if runtime.GOOS == "windows" {
		return exec.Command("schtasks", "/Delete", "/TN", taskName, "/F").Run()
	}
	return removeCrontabEntry(taskName)
}

func registerWindowsTask(taskName, runCmd, cronExpr string) error {
	schedule, interval, startTime, approximated := cronToSchtasks(cronExpr)
	if approximated {
		fmt.Printf("  %s\n", styleWarning.Render("Warning: complex cron expression — approximating to MINUTE/"+interval))
	}
	args := []string{"/Create", "/F", "/TN", taskName, "/TR", runCmd, "/SC", schedule}
	if interval != "" {
		args = append(args, "/MO", interval)
	}
	if startTime != "" {
		args = append(args, "/ST", startTime)
	}
	return exec.Command("schtasks", args...).Run()
}

// cronToSchtasks converts simple cron expressions to schtasks flags.
// Returns approximated=true when a fallback was used.
func cronToSchtasks(expr string) (schedule, interval, startTime string, approximated bool) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return "MINUTE", "60", "", true
	}
	min, hour := fields[0], fields[1]
	// daily at fixed time: "0 8 * * *" — all of dom, month, dow must be wildcards
	if min != "*" && hour != "*" && !strings.Contains(min, "/") && !strings.Contains(hour, "/") &&
		fields[2] == "*" && fields[3] == "*" && fields[4] == "*" {
		h, m := hour, min
		if len(h) == 1 {
			h = "0" + h
		}
		if len(m) == 1 {
			m = "0" + m
		}
		return "DAILY", "", h + ":" + m, false
	}
	// every N minutes: "*/N * * * *"
	if strings.HasPrefix(min, "*/") {
		n := strings.TrimPrefix(min, "*/")
		return "MINUTE", n, "", false
	}
	// hourly: "0 * * * *"
	if min == "0" && hour == "*" {
		return "HOURLY", "", "", false
	}
	return "MINUTE", "60", "", true
}

func registerCrontab(taskName, runCmd, cronExpr string) error {
	entry := fmt.Sprintf("%s %s # moc:%s", cronExpr, runCmd, taskName)
	out, _ := exec.Command("crontab", "-l").Output()
	combined := string(out) + entry + "\n"
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(combined)
	return cmd.Run()
}

func removeCrontabEntry(taskName string) error {
	out, err := exec.Command("crontab", "-l").Output()
	if err != nil {
		return nil
	}
	var kept []string
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "# moc:"+taskName) {
			kept = append(kept, line)
		}
	}
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(strings.Join(kept, "\n"))
	return cmd.Run()
}
