package postgres

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/rs/zerolog"
)

func TestGooseLogger_Fatalf_LogsAtErrorLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	gl := gooseLogger{log: logger}

	gl.Fatalf("migration %d failed: %s", 42, "syntax error")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal log entry: %v", err)
	}

	if entry["level"] != "error" {
		t.Errorf("level = %q, want %q", entry["level"], "error")
	}
	if msg, ok := entry["message"].(string); !ok || msg != "migration 42 failed: syntax error" {
		t.Errorf("message = %q, want %q", entry["message"], "migration 42 failed: syntax error")
	}
}

func TestGooseLogger_Printf_LogsAtInfoLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	gl := gooseLogger{log: logger}

	gl.Printf("applied migration %d", 7)

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal log entry: %v", err)
	}

	if entry["level"] != "info" {
		t.Errorf("level = %q, want %q", entry["level"], "info")
	}
	if msg, ok := entry["message"].(string); !ok || msg != "applied migration 7" {
		t.Errorf("message = %q, want %q", entry["message"], "applied migration 7")
	}
}
