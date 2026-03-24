package status

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Logger struct {
	path string
}

func New(dir string) *Logger {
	if dir == "" {
		dir = "/etc/nerdyagent"
	}
	_ = os.MkdirAll(dir, 0o755)
	return &Logger{path: filepath.Join(dir, "status.log")}
}

func (l *Logger) Write(msg string) {
	if msg == "" {
		return
	}
	timestamp := time.Now().UTC().Format(time.RFC3339)
	line := fmt.Sprintf("%s %s\n", timestamp, strings.TrimSpace(msg))
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err == nil {
		_, _ = f.WriteString(line)
		_ = f.Close()
	}
	fmt.Printf("NRMM STATUS: %s\n", msg)
}
