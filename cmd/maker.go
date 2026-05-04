package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
	makerCronFlag    string
	makerOSFlag      bool
	makerAddFlag     bool
	makerWorkdirFlag string
	makerAllFlag     bool
)

// ── root ─────────────────────────────────────────────────────────────────────

var makerCmd = &cobra.Command{
	Use:   "maker",
	Short: "Personal CLI command repository",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return runMakerShell()
		}
		return runMakerSaveAndExec(args)
	},
}

// ── ls ────────────────────────────────────────────────────────────────────────

var makerLsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List cmdlets and chains",
	RunE:    runMakerLs,
}

func runMakerLs(_ *cobra.Command, _ []string) error {
	cmdlets, _ := ListCmdlets()
	chains, _ := ListChains()
	fmt.Println()
	fmt.Printf("  %s\n\n", styleHeader.Render("CMDLETS"))
	for _, c := range cmdlets {
		cmds, _ := ListCommands(c)
		fmt.Printf("  %-20s %s\n", styleSuccess.Render(c), styleDim.Render(fmt.Sprintf("%d commands", len(cmds))))
	}
	if len(cmdlets) == 0 {
		fmt.Printf("  %s\n", styleDim.Render("None. Use: moc maker <cmdlet> <command>"))
	}
	fmt.Printf("\n  %s\n\n", styleHeader.Render("CHAINS"))
	for _, ch := range chains {
		fmt.Printf("  %-20s %s\n", styleSuccess.Render(ch.Name),
			styleDim.Render(fmt.Sprintf("%d steps · %s", len(ch.Steps), ch.LastStatus)))
	}
	if len(chains) == 0 {
		fmt.Printf("  %s\n", styleDim.Render("None. Use: moc maker chain add <name> <steps...>"))
	}
	fmt.Println()
	return nil
}

// ── run ───────────────────────────────────────────────────────────────────────

var makerRunCmd = &cobra.Command{
	Use:   "run <cmdlet> <slug>",
	Short: "Run a saved command",
	Args:  cobra.ExactArgs(2),
	RunE: func(_ *cobra.Command, args []string) error {
		c, err := LoadCommand(args[0], args[1])
		if err != nil {
			return fmt.Errorf("command not found: %s/%s", args[0], args[1])
		}
		return RunCommand(c, true)
	},
}

// ── log ───────────────────────────────────────────────────────────────────────

var makerLogCmd = &cobra.Command{
	Use:   "log <cmdlet> <slug>",
	Short: "Show log of a command",
	Args:  cobra.ExactArgs(2),
	RunE: func(_ *cobra.Command, args []string) error {
		name := logName(args[0], args[1])
		if makerAllFlag {
			content, err := ReadFullLog(name)
			if err != nil {
				return err
			}
			fmt.Print(content)
			return nil
		}
		lines, err := TailLog(name, 20)
		if err != nil {
			return err
		}
		for _, l := range lines {
			fmt.Println(l)
		}
		return nil
	},
}

// ── schedule ──────────────────────────────────────────────────────────────────

var makerScheduleCmd = &cobra.Command{
	Use:   "schedule <cmdlet> <slug>",
	Short: "Schedule a command",
	Args:  cobra.ExactArgs(2),
	RunE: func(_ *cobra.Command, args []string) error {
		if makerCronFlag == "" {
			return fmt.Errorf("--cron is required")
		}
		if err := ValidateCron(makerCronFlag); err != nil {
			return fmt.Errorf("invalid cron expression: %w", err)
		}
		cmdlet, slug := args[0], args[1]
		c, err := LoadCommand(cmdlet, slug)
		if err != nil {
			return fmt.Errorf("command not found: %s/%s", cmdlet, slug)
		}
		c.Schedule.Cron = makerCronFlag
		c.Schedule.InSession = true
		if makerOSFlag {
			if err := RegisterOSSchedule(cmdlet, slug, makerCronFlag); err != nil {
				fmt.Printf("  %s\n", styleWarning.Render("Warning: failed to register on OS: "+err.Error()))
			} else {
				c.Schedule.OSRegistered = true
			}
		}
		if err := SaveCommand(c); err != nil {
			return err
		}
		fmt.Printf("  %s %s/%s scheduled (%s)\n", styleSuccess.Render("✓"), cmdlet, slug, makerCronFlag)
		return nil
	},
}

// ── unschedule ────────────────────────────────────────────────────────────────

var makerUnscheduleCmd = &cobra.Command{
	Use:   "unschedule <cmdlet> <slug>",
	Short: "Remove schedule",
	Args:  cobra.ExactArgs(2),
	RunE: func(_ *cobra.Command, args []string) error {
		cmdlet, slug := args[0], args[1]
		c, err := LoadCommand(cmdlet, slug)
		if err != nil {
			return fmt.Errorf("command not found: %s/%s", cmdlet, slug)
		}
		if makerOSFlag && c.Schedule.OSRegistered {
			if err := UnregisterOSSchedule(cmdlet, slug); err != nil {
				fmt.Printf("  %s\n", styleWarning.Render("Failed to remove from OS: "+err.Error()))
			} else {
				c.Schedule.OSRegistered = false
			}
		}
		c.Schedule.Cron = ""
		c.Schedule.InSession = false
		if err := SaveCommand(c); err != nil {
			return err
		}
		fmt.Printf("  %s %s/%s schedule removed\n", styleSuccess.Render("✓"), cmdlet, slug)
		return nil
	},
}

// ── backup / restore ──────────────────────────────────────────────────────────

var makerBackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Export commands and chains",
	RunE: func(_ *cobra.Command, _ []string) error {
		dest := filepath.Join(MocDir(), "backup", time.Now().Format("2006-01-02")+".yaml")
		if err := Backup(dest); err != nil {
			return err
		}
		fmt.Printf("  %s %s\n", styleSuccess.Render("✓ Backup:"), styleDim.Render(dest))
		return nil
	},
}

var makerRestoreCmd = &cobra.Command{
	Use:   "restore <file>",
	Short: "Import backup",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		if err := Restore(args[0]); err != nil {
			return err
		}
		fmt.Printf("  %s %s\n", styleSuccess.Render("✓ Restored from:"), styleDim.Render(args[0]))
		return nil
	},
}

// ── chain ─────────────────────────────────────────────────────────────────────

var makerChainCmd = &cobra.Command{
	Use:   "chain",
	Short: "Manage command chains",
}

var makerChainAddCmd = &cobra.Command{
	Use:   "add <name> <cmdlet/slug>...",
	Short: "Create chain",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(_ *cobra.Command, args []string) error {
		name := args[0]
		var steps []ChainStep
		for _, s := range args[1:] {
			steps = append(steps, ChainStep{Command: s})
		}
		chain := &Chain{
			Name:        name,
			StopOnError: true,
			Steps:       steps,
			CreatedAt:   time.Now(),
			LastStatus:  "never",
		}
		if err := SaveChain(chain); err != nil {
			return err
		}
		fmt.Printf("  %s chain %s with %d steps\n", styleSuccess.Render("✓"), name, len(steps))
		return nil
	},
}

var makerChainRunCmd = &cobra.Command{
	Use:   "run <name>",
	Short: "Run chain",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		chain, err := LoadChain(args[0])
		if err != nil {
			return fmt.Errorf("chain not found: %s", args[0])
		}
		return RunChain(chain, true)
	},
}

var makerChainExportCmd = &cobra.Command{
	Use:   "export <name>",
	Short: "Export chain as shell script",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		chain, err := LoadChain(args[0])
		if err != nil {
			return fmt.Errorf("chain not found: %s", args[0])
		}
		fmt.Println("#!/bin/bash")
		if chain.StopOnError {
			fmt.Println("set -e")
		}
		for _, step := range chain.Steps {
			parts := strings.SplitN(step.Command, "/", 2)
			if len(parts) != 2 {
				continue
			}
			c, err := LoadCommand(parts[0], parts[1])
			if err != nil {
				fmt.Printf("# step %s not found\n", step.Command)
				continue
			}
			fmt.Println(c.Command)
		}
		return nil
	},
}

// ── dynamic save+exec ─────────────────────────────────────────────────────────

// runMakerSaveAndExec handles: moc maker <cmdlet> <command words...>
// Same command → exec only. Changed command → update+save+exec. --add → save only.
func runMakerSaveAndExec(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: moc maker <cmdlet> <command>")
	}
	cmdlet := args[0]
	rest := strings.Join(args[1:], " ")
	// normalize: ensure command always starts with cmdlet so "git status" and
	// passing ["git","status"] both resolve to the same stored entry.
	var command string
	if strings.HasPrefix(strings.ToLower(rest), strings.ToLower(cmdlet)+" ") || strings.EqualFold(rest, cmdlet) {
		command = rest
	} else {
		command = cmdlet + " " + rest
	}
	slug := CommandSlug(cmdlet, command)

	existing, _ := LoadCommand(cmdlet, slug)
	if existing != nil && existing.Command == command && !makerAddFlag {
		return RunCommand(existing, true)
	}

	workdir := makerWorkdirFlag
	if workdir == "" {
		workdir, _ = os.Getwd()
	}

	c := &Command{
		Cmdlet:     cmdlet,
		Command:    command,
		Type:       "shell",
		Workdir:    workdir,
		CreatedAt:  time.Now(),
		LastStatus: "never",
	}
	if existing != nil {
		c.Schedule = existing.Schedule
		c.Description = existing.Description
		c.CreatedAt = existing.CreatedAt
	}
	if err := SaveCommand(c); err != nil {
		return err
	}
	fmt.Printf("  %s %s/%s\n", styleDim.Render("saved:"), cmdlet, slug)
	if makerAddFlag {
		return nil
	}
	return RunCommand(c, true)
}

// ── init ──────────────────────────────────────────────────────────────────────

func init() {
	makerCmd.Flags().BoolVar(&makerAddFlag, "add", false, "Save only, don't run")
	makerCmd.Flags().StringVar(&makerWorkdirFlag, "workdir", "", "Working directory")

	makerLogCmd.Flags().BoolVar(&makerAllFlag, "all", false, "Show full log")

	makerScheduleCmd.Flags().StringVar(&makerCronFlag, "cron", "", "Cron expression (required)")
	makerScheduleCmd.Flags().BoolVar(&makerOSFlag, "os", false, "Register on OS (cron/schtasks)")
	makerScheduleCmd.MarkFlagRequired("cron")

	makerUnscheduleCmd.Flags().BoolVar(&makerOSFlag, "os", false, "Also remove from OS")

	makerChainCmd.AddCommand(makerChainAddCmd)
	makerChainCmd.AddCommand(makerChainRunCmd)
	makerChainCmd.AddCommand(makerChainExportCmd)

	makerCmd.AddCommand(makerLsCmd)
	makerCmd.AddCommand(makerRunCmd)
	makerCmd.AddCommand(makerLogCmd)
	makerCmd.AddCommand(makerScheduleCmd)
	makerCmd.AddCommand(makerUnscheduleCmd)
	makerCmd.AddCommand(makerBackupCmd)
	makerCmd.AddCommand(makerRestoreCmd)
	makerCmd.AddCommand(makerChainCmd)
}
