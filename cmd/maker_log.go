package cmd

import (
	"bufio"
	"errors"
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
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if info.Size() < maxLogBytes {
		return nil
	}
	// shift: .log.3 deleted, .log.2→.log.3, .log.1→.log.2
	os.Remove(filepath.Join(logDir, fmt.Sprintf("%s.log.%d", name, maxRotations)))
	for i := maxRotations - 1; i >= 1; i-- {
		src := filepath.Join(logDir, fmt.Sprintf("%s.log.%d", name, i))
		dst := filepath.Join(logDir, fmt.Sprintf("%s.log.%d", name, i+1))
		if err := os.Rename(src, dst); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
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
	if n <= 0 {
		return nil, nil
	}
	path := filepath.Join(mocDir(), "logs", name+".log")
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
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
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(lines) <= n {
		return lines, nil
	}
	return lines[len(lines)-n:], nil
}

func ReadFullLog(name string) (string, error) {
	path := filepath.Join(mocDir(), "logs", name+".log")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}
