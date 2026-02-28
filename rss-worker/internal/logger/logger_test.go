package logger

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"
)

// captureLog captures log output produced by fn and returns it as a string.
func captureLog(fn func()) string {
	var buf bytes.Buffer
	origOutput := log.Writer()
	origFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0) // remove timestamp for predictable output
	defer func() {
		log.SetOutput(origOutput)
		log.SetFlags(origFlags)
	}()
	fn()
	return buf.String()
}

func TestDebugf_SuppressedAtInfoLevel(t *testing.T) {
	SetLevel(LevelInfo)

	output := captureLog(func() {
		Debugf("should not appear")
	})

	if output != "" {
		t.Errorf("Debugf at INFO level should produce no output, got %q", output)
	}
}

func TestInfof_OutputsAtInfoLevel(t *testing.T) {
	SetLevel(LevelInfo)

	output := captureLog(func() {
		Infof("hello %s", "world")
	})

	if !strings.Contains(output, "[INFO]") {
		t.Errorf("Infof output should contain [INFO], got %q", output)
	}
	if !strings.Contains(output, "hello world") {
		t.Errorf("Infof output should contain message, got %q", output)
	}
}

func TestDebugf_OutputsAtDebugLevel(t *testing.T) {
	SetLevel(LevelDebug)

	output := captureLog(func() {
		Debugf("debug message %d", 42)
	})

	if !strings.Contains(output, "[DEBUG]") {
		t.Errorf("Debugf output should contain [DEBUG], got %q", output)
	}
	if !strings.Contains(output, "debug message 42") {
		t.Errorf("Debugf output should contain message, got %q", output)
	}
}

func TestWarnf_OutputsAtWarnLevel(t *testing.T) {
	SetLevel(LevelWarn)

	output := captureLog(func() {
		Warnf("warning: %s", "low disk")
	})

	if !strings.Contains(output, "[WARN]") {
		t.Errorf("Warnf output should contain [WARN], got %q", output)
	}
	if !strings.Contains(output, "warning: low disk") {
		t.Errorf("Warnf output should contain message, got %q", output)
	}
}

func TestErrorf_OutputsAtErrorLevel(t *testing.T) {
	SetLevel(LevelError)

	output := captureLog(func() {
		Errorf("something broke: %v", "timeout")
	})

	if !strings.Contains(output, "[ERROR]") {
		t.Errorf("Errorf output should contain [ERROR], got %q", output)
	}
	if !strings.Contains(output, "something broke: timeout") {
		t.Errorf("Errorf output should contain message, got %q", output)
	}
}

func TestInfof_SuppressedAtWarnLevel(t *testing.T) {
	SetLevel(LevelWarn)

	output := captureLog(func() {
		Infof("should not appear")
	})

	if output != "" {
		t.Errorf("Infof at WARN level should produce no output, got %q", output)
	}
}

func TestWarnf_SuppressedAtErrorLevel(t *testing.T) {
	SetLevel(LevelError)

	output := captureLog(func() {
		Warnf("should not appear")
	})

	if output != "" {
		t.Errorf("Warnf at ERROR level should produce no output, got %q", output)
	}
}

func TestParseLevel_CaseInsensitive(t *testing.T) {
	tests := []struct {
		input string
		want  Level
	}{
		{"debug", LevelDebug},
		{"DEBUG", LevelDebug},
		{"Debug", LevelDebug},
		{"info", LevelInfo},
		{"INFO", LevelInfo},
		{"warn", LevelWarn},
		{"WARN", LevelWarn},
		{"error", LevelError},
		{"ERROR", LevelError},
		{"fatal", LevelFatal},
		{"FATAL", LevelFatal},
		{"", LevelInfo},
		{"unknown", LevelInfo},
		{"  DEBUG  ", LevelDebug},
	}

	for _, tt := range tests {
		got := parseLevel(tt.input)
		if got != tt.want {
			t.Errorf("parseLevel(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestInit_ReadsLOG_LEVEL(t *testing.T) {
	origLevel := os.Getenv("LOG_LEVEL")
	defer os.Setenv("LOG_LEVEL", origLevel)

	os.Setenv("LOG_LEVEL", "DEBUG")
	// Re-run init logic manually
	currentLevel = parseLevel(os.Getenv("LOG_LEVEL"))

	if currentLevel != LevelDebug {
		t.Errorf("currentLevel = %d after LOG_LEVEL=DEBUG, want %d", currentLevel, LevelDebug)
	}
}

func TestInit_DefaultsToInfo(t *testing.T) {
	origLevel := os.Getenv("LOG_LEVEL")
	defer os.Setenv("LOG_LEVEL", origLevel)

	os.Unsetenv("LOG_LEVEL")
	currentLevel = parseLevel(os.Getenv("LOG_LEVEL"))

	if currentLevel != LevelInfo {
		t.Errorf("currentLevel = %d with no LOG_LEVEL, want %d", currentLevel, LevelInfo)
	}
}
