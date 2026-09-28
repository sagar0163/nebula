package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// LogEvent represents a structured observability log entry
type LogEvent struct {
	InstanceID string `json:"instance_id"`
	Phase      string `json:"phase"`
	Tool       string `json:"tool,omitempty"`
	DurationMs int64  `json:"duration_ms"`
	TokensIn   int    `json:"tokens_in,omitempty"`
	TokensOut  int    `json:"tokens_out,omitempty"`
	Result     string `json:"result,omitempty"`
	Error      string `json:"error,omitempty"`
	Timestamp  string `json:"timestamp"`
}

// StructuredLogger handles writing JSONL logs
type StructuredLogger struct {
	file    *os.File
	encoder *json.Encoder
}

// NewStructuredLogger creates a new JSONL logger
func NewStructuredLogger(workDir string) (*StructuredLogger, error) {
	nebulaDir := filepath.Join(workDir, ".nebula")
	if err := os.MkdirAll(nebulaDir, 0755); err != nil {
		return nil, err
	}

	path := filepath.Join(nebulaDir, "steps.jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	return &StructuredLogger{
		file:    file,
		encoder: json.NewEncoder(file),
	}, nil
}

// Log writes an event to the JSONL log
func (l *StructuredLogger) Log(event LogEvent) error {
	event.Timestamp = time.Now().Format(time.RFC3339)
	return l.encoder.Encode(event)
}

// Close closes the underlying log file
func (l *StructuredLogger) Close() error {
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}
