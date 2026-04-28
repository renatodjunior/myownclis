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
