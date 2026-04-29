package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// RunCommand executes a saved command. liveOutput=true streams to terminal;
// false captures output for the notification channel.
func RunCommand(c *Command, liveOutput bool) error {
	name := logName(c.Cmdlet, CommandSlug(c.Cmdlet, c.Command))
	start := time.Now()
	AppendLog(name, "START", c.Command, start)

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", c.Command)
	} else {
		cmd = exec.Command("sh", "-c", c.Command)
	}
	if c.Workdir != "" {
		cmd.Dir = c.Workdir
	}

	var buf bytes.Buffer
	if liveOutput {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stdout = &buf
		cmd.Stderr = &buf
	}

	err := cmd.Run()
	status := "success"
	if err != nil {
		status = "failed"
	}

	AppendLog(name,
		strings.ToUpper(status)+" ("+fmt.Sprintf("%.1fs", time.Since(start).Seconds())+")",
		buf.String(), time.Now())

	now := time.Now()
	c.LastRun = &now
	c.LastStatus = status
	SaveCommand(c)

	return err
}

// RunChain executes chain steps in order. Stops on first error if StopOnError=true.
func RunChain(chain *Chain, liveOutput bool) error {
	chainLog := "chain-" + chain.Name
	start := time.Now()
	AppendLog(chainLog, "START", chain.Name, start)

	for _, step := range chain.Steps {
		parts := strings.SplitN(step.Command, "/", 2)
		if len(parts) != 2 {
			AppendLog(chainLog, "SKIP", "invalid step ref: "+step.Command, time.Now())
			continue
		}
		c, err := LoadCommand(parts[0], parts[1])
		if err != nil {
			msg := fmt.Sprintf("step %s not found: %v", step.Command, err)
			AppendLog(chainLog, "ERROR", msg, time.Now())
			if chain.StopOnError {
				chain.LastStatus = "failed"
				now := time.Now()
				chain.LastRun = &now
				SaveChain(chain)
				return fmt.Errorf("%s", msg)
			}
			continue
		}
		AppendLog(chainLog, "STEP", c.Command, time.Now())
		if err := RunCommand(c, liveOutput); err != nil {
			AppendLog(chainLog, "STEP FAILED", err.Error(), time.Now())
			if chain.StopOnError {
				chain.LastStatus = "failed"
				now := time.Now()
				chain.LastRun = &now
				SaveChain(chain)
				return err
			}
		}
	}

	now := time.Now()
	chain.LastRun = &now
	chain.LastStatus = "success"
	SaveChain(chain)
	AppendLog(chainLog, "SUCCESS", fmt.Sprintf("%.1fs", time.Since(start).Seconds()), time.Now())
	return nil
}
