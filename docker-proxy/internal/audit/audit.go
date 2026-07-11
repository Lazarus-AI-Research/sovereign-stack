// Package audit writes an append-only JSONL record of every proxy decision
// (design.md §3.6: audit all actions).
package audit

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Entry struct {
	Time     string `json:"time"`
	Method   string `json:"method"`
	Path     string `json:"path"`
	Decision string `json:"decision"` // allow | deny | error
	Reason   string `json:"reason,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

type Logger struct {
	mu sync.Mutex
	w  io.Writer
}

// Open appends to path, creating parent directories. An unwritable audit log
// is fatal by design: no audit, no proxy.
func Open(path string) (*Logger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("audit: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("audit: %w", err)
	}
	return &Logger{w: file}, nil
}

func NewWithWriter(w io.Writer) *Logger { return &Logger{w: w} }

func (l *Logger) Log(entry Entry) {
	if entry.Time == "" {
		entry.Time = time.Now().UTC().Format(time.RFC3339)
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.w.Write(append(line, '\n'))
}
