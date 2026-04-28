# Maker Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the `maker` module to `moc` — a personal command repository that saves, executes, schedules, and chains shell/moc commands grouped by cmdlet namespace.

**Architecture:** Five new files handle store, logging, execution, scheduling, and TUI independently. `maker.go` wires all cobra subcommands. `root.go` gets minor changes to register `makerCmd`, start the in-session scheduler on boot, and drain live notifications between prompts. All state lives in `~/.moc/` as YAML files.

**Tech Stack:** Go 1.24, Cobra, `go.yaml.in/yaml/v3`, `github.com/robfig/cron/v3`, `charmbracelet/bubbletea v1.3.10`, `charmbracelet/lipgloss` (all styles reuse existing vars from `sf_styles.go`)

---

## File Map

| File | Status | Responsibility |
|------|--------|----------------|
| `cmd/maker_store.go` | Create | Command + Chain structs, CRUD, slug generation, backup |
| `cmd/maker_log.go` | Create | Log append, rotation (1MB/3 files), tail, cleanup |
| `cmd/maker_exec.go` | Create | RunCommand + RunChain with live output + log write |
| `cmd/maker_scheduler.go` | Create | In-session cron goroutines, OS schtasks/crontab registration |
| `cmd/maker_tui.go` | Create | Bubbletea model for interactive maker shell |
| `cmd/maker.go` | Create | All cobra subcommands + dynamic cmdlet routing |
| `cmd/maker_store_test.go` | Create | Store unit tests with temp dir override |
| `cmd/maker_log_test.go` | Create | Log unit tests with temp dir override |
| `cmd/root.go` | Modify | Register makerCmd, start scheduler, drain NotifyCh |

---

### Task 1: Add dependencies

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add robfig/cron and promote bubbletea + yaml to direct deps**

```bash
cd "c:/Users/Windows 11/Projetos/MyOwnCLI"
go get github.com/robfig/cron/v3
go get github.com/charmbracelet/bubbletea
go get go.yaml.in/yaml/v3
go mod tidy
```

- [ ] **Step 2: Verify direct deps in go.mod**

```bash
grep -E "robfig|bubbletea v|yaml/v3" go.mod
```

Expected: three lines without `// indirect`

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add robfig/cron/v3, promote bubbletea and yaml to direct deps"
```

---

### Task 2: Store — structs, path helpers, slug, tests

**Files:**
- Create: `cmd/maker_store.go`
- Create: `cmd/maker_store_test.go`

- [ ] **Step 1: Write failing tests**

Create `cmd/maker_store_test.go`:

```go
package cmd

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMocDir(t *testing.T) {
	dir := MocDir()
	if !strings.HasSuffix(dir, ".moc") {
		t.Errorf("expected path ending in .moc, got %s", dir)
	}
}

func TestCommandSlug(t *testing.T) {
	tests := []struct{ cmdlet, command, want string }{
		{"git", "git pull", "pull"},
		{"git", "git reset --soft HEAD~3", "reset-soft-head-3"},
		{"go", "go run .", "run"},
		{"aws", "aws s3 ls", "s3-ls"},
	}
	for _, tt := range tests {
		got := CommandSlug(tt.cmdlet, tt.command)
		if got != tt.want {
			t.Errorf("CommandSlug(%q, %q) = %q, want %q", tt.cmdlet, tt.command, got, tt.want)
		}
	}
}

func TestCommandPath(t *testing.T) {
	p := CommandPath("git", "pull")
	if !strings.Contains(p, filepath.Join("commands", "git", "pull.yaml")) {
		t.Errorf("unexpected path: %s", p)
	}
}

func TestSaveAndLoadCommand(t *testing.T) {
	dir := t.TempDir()
	origOverride := mocDirOverride
	mocDirOverride = dir
	defer func() { mocDirOverride = origOverride }()

	c := &Command{
		Cmdlet:     "git",
		Command:    "git pull",
		Type:       "shell",
		CreatedAt:  time.Now(),
		LastStatus: "never",
	}
	if err := SaveCommand(c); err != nil {
		t.Fatalf("SaveCommand: %v", err)
	}
	got, err := LoadCommand("git", "pull")
	if err != nil {
		t.Fatalf("LoadCommand: %v", err)
	}
	if got.Command != "git pull" {
		t.Errorf("got %q, want %q", got.Command, "git pull")
	}
}

func TestListCmdletsAndCommands(t *testing.T) {
	dir := t.TempDir()
	origOverride := mocDirOverride
	mocDirOverride = dir
	defer func() { mocDirOverride = origOverride }()

	for _, c := range []*Command{
		{Cmdlet: "git", Command: "git pull", Type: "shell", CreatedAt: time.Now(), LastStatus: "never"},
		{Cmdlet: "git", Command: "git push", Type: "shell", CreatedAt: time.Now(), LastStatus: "never"},
		{Cmdlet: "go", Command: "go run .", Type: "shell", CreatedAt: time.Now(), LastStatus: "never"},
	} {
		if err := SaveCommand(c); err != nil {
			t.Fatalf("SaveCommand: %v", err)
		}
	}

	cmdlets, err := ListCmdlets()
	if err != nil {
		t.Fatalf("ListCmdlets: %v", err)
	}
	if len(cmdlets) != 2 {
		t.Errorf("expected 2 cmdlets, got %d: %v", len(cmdlets), cmdlets)
	}

	cmds, err := ListCommands("git")
	if err != nil {
		t.Fatalf("ListCommands: %v", err)
	}
	if len(cmds) != 2 {
		t.Errorf("expected 2 git commands, got %d", len(cmds))
	}
}

func TestDeleteCommand(t *testing.T) {
	dir := t.TempDir()
	origOverride := mocDirOverride
	mocDirOverride = dir
	defer func() { mocDirOverride = origOverride }()

	SaveCommand(&Command{Cmdlet: "git", Command: "git pull", Type: "shell", CreatedAt: time.Now(), LastStatus: "never"})
	if err := DeleteCommand("git", "pull"); err != nil {
		t.Fatalf("DeleteCommand: %v", err)
	}
	if _, err := LoadCommand("git", "pull"); err == nil {
		t.Error("expected error after delete")
	}
}

func TestSaveAndLoadChain(t *testing.T) {
	dir := t.TempDir()
	origOverride := mocDirOverride
	mocDirOverride = dir
	defer func() { mocDirOverride = origOverride }()

	chain := &Chain{
		Name:        "deploy",
		StopOnError: true,
		Steps:       []ChainStep{{Command: "git/pull"}, {Command: "go/run"}},
		CreatedAt:   time.Now(),
		LastStatus:  "never",
	}
	if err := SaveChain(chain); err != nil {
		t.Fatalf("SaveChain: %v", err)
	}
	got, err := LoadChain("deploy")
	if err != nil {
		t.Fatalf("LoadChain: %v", err)
	}
	if len(got.Steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(got.Steps))
	}
}

func TestBackupAndRestore(t *testing.T) {
	dir := t.TempDir()
	origOverride := mocDirOverride
	mocDirOverride = dir
	defer func() { mocDirOverride = origOverride }()

	SaveCommand(&Command{Cmdlet: "git", Command: "git pull", Type: "shell", CreatedAt: time.Now(), LastStatus: "never"})
	SaveChain(&Chain{Name: "deploy", StopOnError: true, CreatedAt: time.Now(), LastStatus: "never"})

	destDir := t.TempDir()
	backupFile := destDir + "/backup.yaml"
	if err := Backup(backupFile); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	mocDirOverride = t.TempDir()
	if err := Restore(backupFile); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	cmds, _ := ListCommands("git")
	if len(cmds) != 1 {
		t.Errorf("expected 1 command after restore, got %d", len(cmds))
	}
}
```

- [ ] **Step 2: Run to verify failures**

```bash
go test ./cmd/... -run "TestMocDir|TestCommandSlug|TestCommandPath|TestSaveAndLoad|TestList|TestDelete|TestChain|TestBackup" -v 2>&1 | head -30
```

Expected: FAIL — undefined symbols

- [ ] **Step 3: Create cmd/maker_store.go**

```go
package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"go.yaml.in/yaml/v3"
)

// mocDirOverride is empty in production; tests set it to a temp dir.
var mocDirOverride string

func mocDir() string {
	if mocDirOverride != "" {
		return mocDirOverride
	}
	return MocDir()
}

func MocDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".moc")
}

func CommandPath(cmdlet, slug string) string {
	return filepath.Join(mocDir(), "commands", cmdlet, slug+".yaml")
}

func ChainPath(name string) string {
	return filepath.Join(mocDir(), "chains", name+".yaml")
}

func LogPath(name string) string {
	return filepath.Join(mocDir(), "logs", name+".log")
}

// CommandSlug derives a filename slug from a command string.
// "git pull" with cmdlet "git" → "pull"
// "git reset --soft HEAD~3" with cmdlet "git" → "reset-soft-head-3"
func CommandSlug(cmdlet, command string) string {
	words := strings.Fields(command)
	if len(words) > 0 && strings.EqualFold(words[0], cmdlet) {
		words = words[1:]
	}
	if len(words) == 0 {
		return strings.ToLower(cmdlet)
	}
	raw := strings.ToLower(strings.Join(words, "-"))
	var b strings.Builder
	prevDash := false
	for _, r := range raw {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			b.WriteRune('-')
			prevDash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "cmd"
	}
	return s
}

// ── types ────────────────────────────────────────────────────────────────────

type MakerSchedule struct {
	Cron         string `yaml:"cron"`
	InSession    bool   `yaml:"in_session"`
	OSRegistered bool   `yaml:"os_registered"`
}

type Command struct {
	Cmdlet      string        `yaml:"cmdlet"`
	Command     string        `yaml:"command"`
	Description string        `yaml:"description,omitempty"`
	Type        string        `yaml:"type"`
	Workdir     string        `yaml:"workdir,omitempty"`
	CreatedAt   time.Time     `yaml:"created_at"`
	LastRun     *time.Time    `yaml:"last_run,omitempty"`
	LastStatus  string        `yaml:"last_status"`
	Schedule    MakerSchedule `yaml:"schedule"`
}

type ChainStep struct {
	Command string `yaml:"command"` // "cmdlet/slug" format
}

type Chain struct {
	Name        string        `yaml:"name"`
	Description string        `yaml:"description,omitempty"`
	StopOnError bool          `yaml:"stop_on_error"`
	Steps       []ChainStep   `yaml:"steps"`
	CreatedAt   time.Time     `yaml:"created_at"`
	LastRun     *time.Time    `yaml:"last_run,omitempty"`
	LastStatus  string        `yaml:"last_status"`
	Schedule    MakerSchedule `yaml:"schedule"`
}

// ── command CRUD ──────────────────────────────────────────────────────────────

func SaveCommand(c *Command) error {
	slug := CommandSlug(c.Cmdlet, c.Command)
	path := filepath.Join(mocDir(), "commands", c.Cmdlet, slug+".yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return yaml.NewEncoder(f).Encode(c)
}

func LoadCommand(cmdlet, slug string) (*Command, error) {
	path := filepath.Join(mocDir(), "commands", cmdlet, slug+".yaml")
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var c Command
	if err := yaml.NewDecoder(f).Decode(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

func ListCmdlets() ([]string, error) {
	base := filepath.Join(mocDir(), "commands")
	entries, err := os.ReadDir(base)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

func ListCommands(cmdlet string) ([]*Command, error) {
	dir := filepath.Join(mocDir(), "commands", cmdlet)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Command
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
			slug := strings.TrimSuffix(e.Name(), ".yaml")
			c, err := LoadCommand(cmdlet, slug)
			if err != nil {
				continue
			}
			out = append(out, c)
		}
	}
	return out, nil
}

func DeleteCommand(cmdlet, slug string) error {
	return os.Remove(filepath.Join(mocDir(), "commands", cmdlet, slug+".yaml"))
}

// ── chain CRUD ────────────────────────────────────────────────────────────────

func SaveChain(chain *Chain) error {
	path := filepath.Join(mocDir(), "chains", chain.Name+".yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return yaml.NewEncoder(f).Encode(chain)
}

func LoadChain(name string) (*Chain, error) {
	path := filepath.Join(mocDir(), "chains", name+".yaml")
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var chain Chain
	if err := yaml.NewDecoder(f).Decode(&chain); err != nil {
		return nil, err
	}
	return &chain, nil
}

func ListChains() ([]*Chain, error) {
	dir := filepath.Join(mocDir(), "chains")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Chain
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
			chain, err := LoadChain(strings.TrimSuffix(e.Name(), ".yaml"))
			if err != nil {
				continue
			}
			out = append(out, chain)
		}
	}
	return out, nil
}

func DeleteChain(name string) error {
	return os.Remove(filepath.Join(mocDir(), "chains", name+".yaml"))
}

// ── backup / restore ──────────────────────────────────────────────────────────

type BackupFile struct {
	Commands []*Command `yaml:"commands"`
	Chains   []*Chain   `yaml:"chains"`
}

func Backup(destPath string) error {
	var all BackupFile
	cmdlets, _ := ListCmdlets()
	for _, cmdlet := range cmdlets {
		cmds, _ := ListCommands(cmdlet)
		all.Commands = append(all.Commands, cmds...)
	}
	all.Chains, _ = ListChains()
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}
	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return yaml.NewEncoder(f).Encode(all)
}

func Restore(srcPath string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()
	var bk BackupFile
	if err := yaml.NewDecoder(f).Decode(&bk); err != nil {
		return err
	}
	for _, c := range bk.Commands {
		if err := SaveCommand(c); err != nil {
			return err
		}
	}
	for _, ch := range bk.Chains {
		if err := SaveChain(ch); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./cmd/... -run "TestMocDir|TestCommandSlug|TestCommandPath|TestSaveAndLoad|TestList|TestDelete|TestChain|TestBackup" -v
```

Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/maker_store.go cmd/maker_store_test.go
git commit -m "feat(maker): add store — Command/Chain CRUD, slug, backup/restore"
```

---

### Task 3: Log writer, rotation, tail

**Files:**
- Create: `cmd/maker_log.go`
- Create: `cmd/maker_log_test.go`

- [ ] **Step 1: Write failing tests**

Create `cmd/maker_log_test.go`:

```go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendAndTailLog(t *testing.T) {
	dir := t.TempDir()
	origOverride := mocDirOverride
	mocDirOverride = dir
	defer func() { mocDirOverride = origOverride }()

	for i := 0; i < 25; i++ {
		if err := AppendLog("git-pull", "run", fmt.Sprintf("line %d", i), time.Now()); err != nil {
			t.Fatalf("AppendLog: %v", err)
		}
	}

	lines, err := TailLog("git-pull", 10)
	if err != nil {
		t.Fatalf("TailLog: %v", err)
	}
	if len(lines) != 10 {
		t.Errorf("expected 10 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[9], "line 24") {
		t.Errorf("last line should be line 24, got: %s", lines[9])
	}
}

func TestRotateLog(t *testing.T) {
	dir := t.TempDir()
	origOverride := mocDirOverride
	mocDirOverride = dir
	defer func() { mocDirOverride = origOverride }()

	logDir := filepath.Join(dir, "logs")
	os.MkdirAll(logDir, 0755)
	path := filepath.Join(logDir, "git-pull.log")
	f, _ := os.Create(path)
	f.Write(make([]byte, 1024*1024+1)) // > 1MB
	f.Close()

	if err := RotateLog("git-pull"); err != nil {
		t.Fatalf("RotateLog: %v", err)
	}
	if _, err := os.Stat(filepath.Join(logDir, "git-pull.log.1")); err != nil {
		t.Error("expected .log.1 to exist after rotation")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Error("expected new git-pull.log to exist after rotation")
	} else if info.Size() != 0 {
		t.Errorf("new log should be empty, got size %d", info.Size())
	}
}
```

- [ ] **Step 2: Run to verify failures**

```bash
go test ./cmd/... -run "TestAppendAndTailLog|TestRotateLog" -v 2>&1 | head -20
```

Expected: FAIL — `AppendLog`, `TailLog`, `RotateLog` undefined

- [ ] **Step 3: Create cmd/maker_log.go**

```go
package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxLogBytes = 1024 * 1024 // 1MB
const maxRotations = 3

func logName(cmdlet, slug string) string {
	return cmdlet + "-" + slug
}

func AppendLog(name, event, content string, t time.Time) error {
	logDir := filepath.Join(mocDir(), "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}
	path := filepath.Join(logDir, name+".log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	line := fmt.Sprintf("[%s] %s — %s", t.Format("2006-01-02 15:04:05"), name, event)
	if content != "" {
		line += "\n" + content
	}
	_, err = fmt.Fprintln(f, line)
	return err
}

func RotateLog(name string) error {
	logDir := filepath.Join(mocDir(), "logs")
	base := filepath.Join(logDir, name+".log")
	info, err := os.Stat(base)
	if err != nil || info.Size() < maxLogBytes {
		return nil
	}
	// shift: .log.3 deleted, .log.2→.log.3, .log.1→.log.2
	os.Remove(filepath.Join(logDir, fmt.Sprintf("%s.log.%d", name, maxRotations)))
	for i := maxRotations - 1; i >= 1; i-- {
		src := filepath.Join(logDir, fmt.Sprintf("%s.log.%d", name, i))
		dst := filepath.Join(logDir, fmt.Sprintf("%s.log.%d", name, i+1))
		os.Rename(src, dst)
	}
	if err := os.Rename(base, filepath.Join(logDir, name+".log.1")); err != nil {
		return err
	}
	f, err := os.Create(base)
	if err != nil {
		return err
	}
	f.Close()
	return nil
}

func CleanupLogs() {
	logDir := filepath.Join(mocDir(), "logs")
	entries, _ := os.ReadDir(logDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".log") && !strings.Contains(e.Name(), ".log.") {
			RotateLog(strings.TrimSuffix(e.Name(), ".log"))
		}
	}
}

func TailLog(name string, n int) ([]string, error) {
	path := filepath.Join(mocDir(), "logs", name+".log")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if len(lines) <= n {
		return lines, nil
	}
	return lines[len(lines)-n:], nil
}

func ReadFullLog(name string) (string, error) {
	path := filepath.Join(mocDir(), "logs", name+".log")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./cmd/... -run "TestAppendAndTailLog|TestRotateLog" -v
```

Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/maker_log.go cmd/maker_log_test.go
git commit -m "feat(maker): add log writer with rotation, tail, cleanup"
```

---

### Task 4: Command + chain executor

**Files:**
- Create: `cmd/maker_exec.go`

- [ ] **Step 1: Create cmd/maker_exec.go**

```go
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
```

- [ ] **Step 2: Build to verify it compiles**

```bash
go build ./cmd/...
```

Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add cmd/maker_exec.go
git commit -m "feat(maker): add RunCommand and RunChain with logging"
```

---

### Task 5: In-session scheduler

**Files:**
- Create: `cmd/maker_scheduler.go`

- [ ] **Step 1: Create cmd/maker_scheduler.go**

```go
package cmd

import (
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
			c := c
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
				NotifyCh <- Notification{
					Name:   c.Cmdlet + " " + c.Command,
					Output: strings.Join(lines, "\n"),
					Status: status,
					RunAt:  time.Now(),
				}
			})
		}
	}

	chains, _ := ListChains()
	for _, ch := range chains {
		ch := ch
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
			NotifyCh <- Notification{
				Name:   "chain:" + ch.Name,
				Output: strings.Join(lines, "\n"),
				Status: status,
				RunAt:  time.Now(),
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
```

- [ ] **Step 2: Build to verify it compiles**

```bash
go build ./cmd/...
```

Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add cmd/maker_scheduler.go
git commit -m "feat(maker): add in-session cron scheduler with notification channel"
```

---

### Task 6: OS scheduler (Windows + Linux/Mac)

**Files:**
- Modify: `cmd/maker_scheduler.go`

- [ ] **Step 1: Add OS registration functions to maker_scheduler.go**

Append to `cmd/maker_scheduler.go` (add `"fmt"`, `"os/exec"`, `"runtime"` to imports):

```go
import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// ValidateCron parses the cron expression and returns an error if invalid.
func ValidateCron(expr string) error {
	p := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	_, err := p.Parse(expr)
	return err
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
		fmt.Printf("  %s\n", styleWarning.Render("Aviso: expressão cron complexa — aproximando para MINUTE/"+interval))
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
	// daily at fixed time: "0 8 * * *"
	if min != "*" && hour != "*" && !strings.Contains(min, "/") && !strings.Contains(hour, "/") {
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
```

**Note:** Replace the entire import block at the top of `maker_scheduler.go` with the full block above. The rest of the file (Notification, NotifyCh, StartInSessionScheduler, StopInSessionScheduler) stays unchanged from Task 5.

- [ ] **Step 2: Build to verify it compiles**

```bash
go build ./cmd/...
```

Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add cmd/maker_scheduler.go
git commit -m "feat(maker): add OS scheduler (schtasks/crontab) with cron validation"
```

---

### Task 7: Core cobra commands

**Files:**
- Create: `cmd/maker.go`

- [ ] **Step 1: Create cmd/maker.go**

```go
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
	Short: "Repositório pessoal de comandos CLI",
	// args present and not a known subcommand → treat as <cmdlet> <command...>
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
	Short:   "Lista cmdlets e chains",
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
		fmt.Printf("  %s\n", styleDim.Render("Nenhum. Use: moc maker <cmdlet> <command>"))
	}
	fmt.Printf("\n  %s\n\n", styleHeader.Render("CHAINS"))
	for _, ch := range chains {
		fmt.Printf("  %-20s %s\n", styleSuccess.Render(ch.Name),
			styleDim.Render(fmt.Sprintf("%d steps · %s", len(ch.Steps), ch.LastStatus)))
	}
	if len(chains) == 0 {
		fmt.Printf("  %s\n", styleDim.Render("Nenhuma. Use: moc maker chain add <nome> <steps...>"))
	}
	fmt.Println()
	return nil
}

// ── run ───────────────────────────────────────────────────────────────────────

var makerRunCmd = &cobra.Command{
	Use:   "run <cmdlet> <slug>",
	Short: "Executa comando salvo",
	Args:  cobra.ExactArgs(2),
	RunE: func(_ *cobra.Command, args []string) error {
		c, err := LoadCommand(args[0], args[1])
		if err != nil {
			return fmt.Errorf("comando não encontrado: %s/%s", args[0], args[1])
		}
		return RunCommand(c, true)
	},
}

// ── log ───────────────────────────────────────────────────────────────────────

var makerLogCmd = &cobra.Command{
	Use:   "log <cmdlet> <slug>",
	Short: "Exibe log de um comando",
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
	Short: "Agenda um comando",
	Args:  cobra.ExactArgs(2),
	RunE: func(_ *cobra.Command, args []string) error {
		if makerCronFlag == "" {
			return fmt.Errorf("--cron é obrigatório")
		}
		if err := ValidateCron(makerCronFlag); err != nil {
			return fmt.Errorf("expressão cron inválida: %w", err)
		}
		cmdlet, slug := args[0], args[1]
		c, err := LoadCommand(cmdlet, slug)
		if err != nil {
			return fmt.Errorf("comando não encontrado: %s/%s", cmdlet, slug)
		}
		c.Schedule.Cron = makerCronFlag
		c.Schedule.InSession = true
		if makerOSFlag {
			if err := RegisterOSSchedule(cmdlet, slug, makerCronFlag); err != nil {
				fmt.Printf("  %s\n", styleWarning.Render("Aviso: falha ao registrar no OS: "+err.Error()))
			} else {
				c.Schedule.OSRegistered = true
			}
		}
		if err := SaveCommand(c); err != nil {
			return err
		}
		fmt.Printf("  %s %s/%s agendado (%s)\n", styleSuccess.Render("✓"), cmdlet, slug, makerCronFlag)
		return nil
	},
}

// ── unschedule ────────────────────────────────────────────────────────────────

var makerUnscheduleCmd = &cobra.Command{
	Use:   "unschedule <cmdlet> <slug>",
	Short: "Remove agendamento",
	Args:  cobra.ExactArgs(2),
	RunE: func(_ *cobra.Command, args []string) error {
		cmdlet, slug := args[0], args[1]
		c, err := LoadCommand(cmdlet, slug)
		if err != nil {
			return fmt.Errorf("comando não encontrado: %s/%s", cmdlet, slug)
		}
		if makerOSFlag && c.Schedule.OSRegistered {
			if err := UnregisterOSSchedule(cmdlet, slug); err != nil {
				fmt.Printf("  %s\n", styleWarning.Render("Falha ao remover do OS: "+err.Error()))
			} else {
				c.Schedule.OSRegistered = false
			}
		}
		c.Schedule.Cron = ""
		c.Schedule.InSession = false
		if err := SaveCommand(c); err != nil {
			return err
		}
		fmt.Printf("  %s %s/%s agendamento removido\n", styleSuccess.Render("✓"), cmdlet, slug)
		return nil
	},
}

// ── backup / restore ──────────────────────────────────────────────────────────

var makerBackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Exporta comandos e chains",
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
	Use:   "restore <arquivo>",
	Short: "Importa backup",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		if err := Restore(args[0]); err != nil {
			return err
		}
		fmt.Printf("  %s %s\n", styleSuccess.Render("✓ Restaurado de:"), styleDim.Render(args[0]))
		return nil
	},
}

// ── chain ─────────────────────────────────────────────────────────────────────

var makerChainCmd = &cobra.Command{
	Use:   "chain",
	Short: "Gerencia chains de comandos",
}

var makerChainAddCmd = &cobra.Command{
	Use:   "add <nome> <cmdlet/slug>...",
	Short: "Cria chain",
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
		fmt.Printf("  %s chain %s com %d steps\n", styleSuccess.Render("✓"), name, len(steps))
		return nil
	},
}

var makerChainRunCmd = &cobra.Command{
	Use:   "run <nome>",
	Short: "Executa chain",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		chain, err := LoadChain(args[0])
		if err != nil {
			return fmt.Errorf("chain não encontrada: %s", args[0])
		}
		return RunChain(chain, true)
	},
}

var makerChainExportCmd = &cobra.Command{
	Use:   "export <nome>",
	Short: "Exporta chain como shell script",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		chain, err := LoadChain(args[0])
		if err != nil {
			return fmt.Errorf("chain não encontrada: %s", args[0])
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
// Saves new/changed commands and executes them (--add skips execution).
func runMakerSaveAndExec(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("uso: moc maker <cmdlet> <command>")
	}
	cmdlet := args[0]
	command := strings.Join(args[1:], " ")
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
	fmt.Printf("  %s %s/%s\n", styleDim.Render("salvo:"), cmdlet, slug)
	if makerAddFlag {
		return nil
	}
	return RunCommand(c, true)
}

// ── init ──────────────────────────────────────────────────────────────────────

func init() {
	makerCmd.Flags().BoolVar(&makerAddFlag, "add", false, "Apenas salva, não executa")
	makerCmd.Flags().StringVar(&makerWorkdirFlag, "workdir", "", "Diretório de trabalho")

	makerLogCmd.Flags().BoolVar(&makerAllFlag, "all", false, "Exibe log completo")

	makerScheduleCmd.Flags().StringVar(&makerCronFlag, "cron", "", "Expressão cron (obrigatório)")
	makerScheduleCmd.Flags().BoolVar(&makerOSFlag, "os", false, "Registrar no OS (cron/schtasks)")
	makerScheduleCmd.MarkFlagRequired("cron")

	makerUnscheduleCmd.Flags().BoolVar(&makerOSFlag, "os", false, "Remover também do OS")

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
```

- [ ] **Step 2: Add runMakerShell stub to maker.go (before init)**

Add this function to `cmd/maker.go` before `func init()`. It will be replaced with the real TUI in Task 9:

```go
// runMakerShell is replaced with the bubbletea TUI in maker_tui.go (Task 9).
func runMakerShell() error { return nil }
```

- [ ] **Step 3: Build to verify it compiles**

```bash
go build ./cmd/...
```

Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add cmd/maker.go
git commit -m "feat(maker): add cobra subcommands — ls, run, log, schedule, backup, chain"
```

---

### Task 8: Wire into root.go

**Files:**
- Modify: `cmd/root.go`

- [ ] **Step 1: Register makerCmd in root.go init()**

In `cmd/root.go`, update `func init()`:

```go
func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.AddCommand(sfCmd)
	rootCmd.AddCommand(makerCmd) // ADD
}
```

- [ ] **Step 2: Update printMainHelp() to include maker**

Find the `modules` slice in `printMainHelp()` and update:

```go
modules := [][]string{
	{"sf", "AWS Step Functions — browser, watch, tail, rerun e mais"},
	{"maker", "Repositório de comandos — salva, agenda e encadeia CLIs"},
}
```

- [ ] **Step 3: Update runMainShell() to start scheduler and drain notifications**

Add the following right after `printBanner()` and before `scanner := bufio.NewScanner(os.Stdin)`:

```go
	CleanupLogs()
	StartInSessionScheduler()
	defer StopInSessionScheduler()

	go func() {
		for n := range NotifyCh {
			icon := styleSuccess.Render("✓")
			if n.Status == "failed" {
				icon = styleError.Render("✗")
			}
			fmt.Printf("\n  %s %s [maker] %s — %s\n",
				icon,
				styleDim.Render(n.RunAt.Format("15:04:05")),
				styleSuccess.Render(n.Name),
				styleDim.Render(n.Status),
			)
			if trimmed := strings.TrimSpace(n.Output); trimmed != "" {
				for _, line := range strings.Split(trimmed, "\n") {
					fmt.Printf("    %s\n", styleDim.Render(line))
				}
			}
			fmt.Print("moc ❯ ")
		}
	}()
```

Make sure `"strings"` is in root.go imports (it already is).

- [ ] **Step 4: Build to verify it compiles**

```bash
go build ./...
```

Expected: no errors

- [ ] **Step 5: Smoke test CLI commands**

```bash
./moc.exe maker ls
./moc.exe maker git "git --version" --add
./moc.exe maker ls
./moc.exe maker run git --version
./moc.exe maker git "git status"
./moc.exe maker log git --version
```

Expected: saves, executes, log shows entries

- [ ] **Step 6: Commit**

```bash
git add cmd/root.go
git commit -m "feat(maker): wire makerCmd into root, start scheduler, drain notifications"
```

---

### Task 9: TUI shell

**Files:**
- Create: `cmd/maker_tui.go`
- Modify: `cmd/maker.go` (replace stub `runMakerShell`)

- [ ] **Step 1: Create cmd/maker_tui.go**

```go
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

	// navigate to cmdlet: /git or just "git"
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

	// header
	summary := ""
	for _, c := range m.cmdlets {
		cmds, _ := ListCommands(c)
		summary += fmt.Sprintf("%s·%d  ", c, len(cmds))
	}
	summary += fmt.Sprintf("chains·%d", len(m.chains))
	b.WriteString(makerBorderStyle.Render("  " + strings.TrimSpace(summary)))
	b.WriteString("\n\n")

	// hint bar
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
```

- [ ] **Step 2: Replace runMakerShell stub in maker.go with real implementation**

In `cmd/maker.go`, replace:
```go
func runMakerShell() error { return nil }
```
with:
```go
func runMakerShell() error {
	p := tea.NewProgram(newMakerModel(), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
```

Also add the bubbletea import to `cmd/maker.go`'s import block:
```go
tea "github.com/charmbracelet/bubbletea"
```

- [ ] **Step 3: Build to verify it compiles**

```bash
go build ./...
```

Expected: no errors

- [ ] **Step 4: Smoke test TUI**

```bash
./moc.exe maker
```

Actions to verify:
1. Header shows cmdlets and chains count
2. Type a cmdlet name (e.g. `git`) → list appears
3. Type `/run 1` → command executes with live output, TUI resumes
4. Type `/log 1` → last 15 log lines appear in status area
5. Type `exit` → shell closes

- [ ] **Step 5: Commit**

```bash
git add cmd/maker_tui.go cmd/maker.go
git commit -m "feat(maker): add bubbletea TUI shell with header, cmdlet nav, /commands"
```

---

### Task 10: Final build + end-to-end validation

**Files:** None (validation only)

- [ ] **Step 1: Run all tests**

```bash
go test ./cmd/... -v
```

Expected: all PASS, no compilation errors

- [ ] **Step 2: Build release binary**

```bash
go build -o moc.exe .
```

Expected: no errors

- [ ] **Step 3: End-to-end walkthrough**

```bash
# Save-only
./moc.exe maker git "git --version" --add
./moc.exe maker git "git status" --add
./moc.exe maker go "go build ./..." --workdir "c:/Users/Windows 11/Projetos/MyOwnCLI" --add

# List
./moc.exe maker ls

# Save + execute (new command)
./moc.exe maker git "git log --oneline -5"

# Explicit run
./moc.exe maker run git --version

# Log
./moc.exe maker log git --version

# Chain
./moc.exe maker chain add git-check git/--version git/status
./moc.exe maker chain run git-check
./moc.exe maker chain export git-check

# Backup
./moc.exe maker backup

# TUI
./moc.exe maker
# navigate: git → /run 1 → /log 1 → exit
```

- [ ] **Step 4: Final commit**

```bash
git add -A
git commit -m "feat(maker): complete maker module — store, log, scheduler, TUI, chains"
```
