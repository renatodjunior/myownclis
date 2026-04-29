package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"
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
	home, err := os.UserHomeDir()
	if err != nil {
		panic(fmt.Sprintf("moc: cannot determine home directory: %v", err))
	}
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
	path := CommandPath(c.Cmdlet, slug)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	enc := yaml.NewEncoder(f)
	if err := enc.Encode(c); err != nil {
		f.Close()
		return err
	}
	if err := enc.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func LoadCommand(cmdlet, slug string) (*Command, error) {
	path := CommandPath(cmdlet, slug)
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
	if errors.Is(err, os.ErrNotExist) {
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
	if errors.Is(err, os.ErrNotExist) {
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
	enc := yaml.NewEncoder(f)
	if err := enc.Encode(chain); err != nil {
		f.Close()
		return err
	}
	if err := enc.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
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
	if errors.Is(err, os.ErrNotExist) {
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
	cmdlets, err := ListCmdlets()
	if err != nil {
		return err
	}
	for _, cmdlet := range cmdlets {
		cmds, err := ListCommands(cmdlet)
		if err != nil {
			return err
		}
		all.Commands = append(all.Commands, cmds...)
	}
	chains, err := ListChains()
	if err != nil {
		return err
	}
	all.Chains = chains
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}
	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	enc := yaml.NewEncoder(f)
	if err := enc.Encode(all); err != nil {
		f.Close()
		return err
	}
	if err := enc.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
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
