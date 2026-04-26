package log_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/noamsto/tmux-state/internal/log"
)

func TestNewWritesJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, log.LevelInfo, log.FormatJSON)
	logger.Info("hello", "key", "value")
	out := buf.String()
	if !strings.Contains(out, `"msg":"hello"`) {
		t.Fatalf("expected JSON output containing msg=hello, got: %s", out)
	}
	if !strings.Contains(out, `"key":"value"`) {
		t.Fatalf("expected key=value in output, got: %s", out)
	}
}

func TestNewRespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, log.LevelWarn, log.FormatText)
	logger.Info("ignored")
	logger.Warn("kept")
	out := buf.String()
	if strings.Contains(out, "ignored") {
		t.Fatalf("info message should be filtered, got: %s", out)
	}
	if !strings.Contains(out, "kept") {
		t.Fatalf("warn message should pass, got: %s", out)
	}
}
