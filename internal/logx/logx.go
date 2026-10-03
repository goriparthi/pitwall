// Package logx is a small persistent logger: one file per component in the state dir, rotated at 5 MB.
package logx

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/goriparthi/pitwall/internal/config"
)

const maxBytes = 5 << 20

type Logger struct {
	mu   sync.Mutex
	file string
	tty  bool
}

func New(name string) *Logger {
	_ = os.MkdirAll(config.StateDir(), 0o700)
	st, _ := os.Stderr.Stat()
	return &Logger{file: filepath.Join(config.StateDir(), name+".log"), tty: st != nil && st.Mode()&os.ModeCharDevice != 0}
}

func (l *Logger) write(level, msg string, kv map[string]any) {
	line := time.Now().UTC().Format(time.RFC3339Nano) + " " + level + " " + msg
	if len(kv) > 0 {
		b, _ := json.Marshal(kv)
		line += " " + string(b)
	}
	line += "\n"
	l.mu.Lock()
	defer l.mu.Unlock()
	if st, err := os.Stat(l.file); err == nil && st.Size() > maxBytes {
		_ = os.Rename(l.file, l.file+".1")
	}
	// logging must never take the service down; a failed write still reaches stderr below
	if f, err := os.OpenFile(l.file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		_, _ = f.WriteString(line)
		_ = f.Close()
	}
	if l.tty || level == "ERROR" {
		fmt.Fprint(os.Stderr, line)
	}
}

func (l *Logger) Info(msg string, kv ...map[string]any)  { l.write("INFO", msg, first(kv)) }
func (l *Logger) Warn(msg string, kv ...map[string]any)  { l.write("WARN", msg, first(kv)) }
func (l *Logger) Error(msg string, kv ...map[string]any) { l.write("ERROR", msg, first(kv)) }
func (l *Logger) File() string                           { return l.file }

func first(kv []map[string]any) map[string]any {
	if len(kv) > 0 {
		return kv[0]
	}
	return nil
}
