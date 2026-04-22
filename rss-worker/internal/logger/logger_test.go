package logger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// captureOutput routes logger output into a buffer for the duration of fn.
func captureOutput(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	SetOutput(&buf)
	defer SetOutput(os.Stderr)
	fn()
	return buf.String()
}

func TestDebugf_SuppressedAtInfoLevel(t *testing.T) {
	SetLevel(LevelInfo)

	output := captureOutput(t, func() {
		Debugf("should not appear")
	})

	if output != "" {
		t.Errorf("Debugf at INFO level should produce no output, got %q", output)
	}
}

func TestInfof_OutputsAtInfoLevel(t *testing.T) {
	SetLevel(LevelInfo)

	output := captureOutput(t, func() {
		Infof("hello %s", "world")
	})

	if !strings.Contains(output, "level=INFO") {
		t.Errorf("Infof output should contain level=INFO, got %q", output)
	}
	if !strings.Contains(output, "hello world") {
		t.Errorf("Infof output should contain message, got %q", output)
	}
}

func TestDebugf_OutputsAtDebugLevel(t *testing.T) {
	SetLevel(LevelDebug)
	defer SetLevel(LevelInfo)

	output := captureOutput(t, func() {
		Debugf("debug message %d", 42)
	})

	if !strings.Contains(output, "level=DEBUG") {
		t.Errorf("Debugf output should contain level=DEBUG, got %q", output)
	}
	if !strings.Contains(output, "debug message 42") {
		t.Errorf("Debugf output should contain message, got %q", output)
	}
}

func TestWarnf_OutputsAtWarnLevel(t *testing.T) {
	SetLevel(LevelWarn)
	defer SetLevel(LevelInfo)

	output := captureOutput(t, func() {
		Warnf("warning: %s", "low disk")
	})

	if !strings.Contains(output, "level=WARN") {
		t.Errorf("Warnf output should contain level=WARN, got %q", output)
	}
	if !strings.Contains(output, "warning: low disk") {
		t.Errorf("Warnf output should contain message, got %q", output)
	}
}

func TestErrorf_OutputsAtErrorLevel(t *testing.T) {
	SetLevel(LevelError)
	defer SetLevel(LevelInfo)

	output := captureOutput(t, func() {
		Errorf("something broke: %v", "timeout")
	})

	if !strings.Contains(output, "level=ERROR") {
		t.Errorf("Errorf output should contain level=ERROR, got %q", output)
	}
	if !strings.Contains(output, "something broke: timeout") {
		t.Errorf("Errorf output should contain message, got %q", output)
	}
}

func TestInfof_SuppressedAtWarnLevel(t *testing.T) {
	SetLevel(LevelWarn)
	defer SetLevel(LevelInfo)

	output := captureOutput(t, func() {
		Infof("should not appear")
	})

	if output != "" {
		t.Errorf("Infof at WARN level should produce no output, got %q", output)
	}
}

func TestWarnf_SuppressedAtErrorLevel(t *testing.T) {
	SetLevel(LevelError)
	defer SetLevel(LevelInfo)

	output := captureOutput(t, func() {
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

func TestJSONFormat_EmitsValidJSON(t *testing.T) {
	origFormat := os.Getenv("LOG_FORMAT")
	defer func() {
		os.Setenv("LOG_FORMAT", origFormat)
		Reinit()
	}()

	os.Setenv("LOG_FORMAT", "json")
	SetLevel(LevelInfo)

	var buf bytes.Buffer
	SetOutput(&buf) // SetOutput calls rebuild() which re-reads LOG_FORMAT
	defer SetOutput(os.Stderr)

	Infof("hello %s", "world")

	got := strings.TrimSpace(buf.String())
	if got == "" {
		t.Fatal("expected JSON output, got empty string")
	}

	var record map[string]any
	if err := json.Unmarshal([]byte(got), &record); err != nil {
		t.Fatalf("output is not valid JSON: %v; raw=%q", err, got)
	}

	if record["level"] != "INFO" {
		t.Errorf("level = %v, want INFO", record["level"])
	}
	if record["msg"] != "hello world" {
		t.Errorf("msg = %v, want %q", record["msg"], "hello world")
	}
}

func TestWith_AttachesFieldsToRecord(t *testing.T) {
	origFormat := os.Getenv("LOG_FORMAT")
	defer func() {
		os.Setenv("LOG_FORMAT", origFormat)
		Reinit()
	}()

	os.Setenv("LOG_FORMAT", "json")
	SetLevel(LevelInfo)

	var buf bytes.Buffer
	SetOutput(&buf)
	defer SetOutput(os.Stderr)

	sub := With("run_id", "abc123", "source_id", "src-1")
	sub.Info("processed", "count", 7)

	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &record); err != nil {
		t.Fatalf("output is not valid JSON: %v; raw=%q", err, buf.String())
	}

	if record["run_id"] != "abc123" {
		t.Errorf("run_id = %v, want abc123", record["run_id"])
	}
	if record["source_id"] != "src-1" {
		t.Errorf("source_id = %v, want src-1", record["source_id"])
	}
	// JSON numbers decode as float64
	if c, ok := record["count"].(float64); !ok || c != 7 {
		t.Errorf("count = %v, want 7", record["count"])
	}
}

func TestInit_ReadsLOG_LEVEL(t *testing.T) {
	origLevel := os.Getenv("LOG_LEVEL")
	defer func() {
		os.Setenv("LOG_LEVEL", origLevel)
		SetLevel(parseLevel(origLevel))
	}()

	os.Setenv("LOG_LEVEL", "DEBUG")
	SetLevel(parseLevel(os.Getenv("LOG_LEVEL")))

	// Verify debug output is emitted
	output := captureOutput(t, func() {
		Debugf("trace")
	})
	if !strings.Contains(output, "level=DEBUG") {
		t.Errorf("after LOG_LEVEL=DEBUG, Debugf should emit; got %q", output)
	}
}

func TestInit_DefaultsToInfo(t *testing.T) {
	origLevel := os.Getenv("LOG_LEVEL")
	defer func() {
		os.Setenv("LOG_LEVEL", origLevel)
		SetLevel(parseLevel(origLevel))
	}()

	os.Unsetenv("LOG_LEVEL")
	SetLevel(parseLevel(os.Getenv("LOG_LEVEL")))

	// Verify debug is suppressed
	output := captureOutput(t, func() {
		Debugf("should be filtered")
	})
	if output != "" {
		t.Errorf("with no LOG_LEVEL, Debugf should be suppressed; got %q", output)
	}
}

// TestCurrentOutput_NilFallback covers the defensive branch where the atomic
// output pointer has never been populated — get() should fall back to
// os.Stderr. init() always sets it, so we temporarily swap in a nil pointer.
func TestCurrentOutput_NilFallback(t *testing.T) {
	saved := output.Load()
	output.Store(nil)
	defer func() {
		if saved != nil {
			output.Store(saved)
		} else {
			setOutput(os.Stderr)
		}
	}()

	if got := currentOutput(); got != os.Stderr {
		t.Errorf("currentOutput with nil pointer = %v, want os.Stderr", got)
	}
}

// TestGet_NilFallback covers the `active == nil` branch — get() should
// return slog.Default() rather than panicking.
func TestGet_NilFallback(t *testing.T) {
	saved := active.Load()
	active.Store(nil)
	defer func() {
		if saved != nil {
			active.Store(saved)
		} else {
			rebuild()
		}
	}()

	if got := get(); got != slog.Default() {
		t.Errorf("get() with nil active = %v, want slog.Default()", got)
	}
}

// TestToSlogLevel_DefaultCase covers the default arm of the switch — an
// out-of-range Level value should map to slog.LevelInfo.
func TestToSlogLevel_DefaultCase(t *testing.T) {
	if got := toSlogLevel(Level(999)); got != slog.LevelInfo {
		t.Errorf("toSlogLevel(999) = %v, want slog.LevelInfo", got)
	}
}

// TestFatalf_ExitsWithStatus1 covers Fatalf by re-running the test binary as
// a subprocess with an env sentinel. The child calls Fatalf, which logs at
// ERROR then os.Exit(1); the parent asserts the exit code and stderr message.
func TestFatalf_ExitsWithStatus1(t *testing.T) {
	if os.Getenv("LOGGER_FATAL_CHILD") == "1" {
		Fatalf("fatal test: %s", "boom")
		return // unreachable
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestFatalf_ExitsWithStatus1")
	cmd.Env = append(os.Environ(), "LOGGER_FATAL_CHILD=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected child to exit with error, got %v", err)
	}
	if code := exitErr.ExitCode(); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "fatal test: boom") {
		t.Errorf("stderr missing fatal message; got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "level=ERROR") {
		t.Errorf("stderr missing level=ERROR; got %q", stderr.String())
	}
}
