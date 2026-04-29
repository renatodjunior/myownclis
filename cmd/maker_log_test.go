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
