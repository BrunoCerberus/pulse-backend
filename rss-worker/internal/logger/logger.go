// Package logger wraps log/slog with a compact printf-style API and an
// env-controlled format switch.
//
// Defaults: text handler on stderr, INFO level. Set LOG_LEVEL to one of
// DEBUG/INFO/WARN/ERROR/FATAL to adjust the filter. Set LOG_FORMAT=json to
// emit structured JSON instead of slog's text format — useful for log
// aggregators (Loki, Datadog, etc.).
//
// Call With(key, value, ...) to get a *slog.Logger pre-populated with
// context fields (run_id, source_id, url_hash, ...). Use it for per-source
// or per-article log sites where structured correlation is worth the ceremony.
package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
)

// Level represents a logging severity level.
type Level int

const (
	LevelDebug Level = 0
	LevelInfo  Level = 1
	LevelWarn  Level = 2
	LevelError Level = 3
	LevelFatal Level = 4
)

var (
	levelVar slog.LevelVar
	active   atomic.Pointer[slog.Logger]
	output   atomic.Pointer[io.Writer]
)

func init() {
	levelVar.Set(toSlogLevel(parseLevel(os.Getenv("LOG_LEVEL"))))
	setOutput(os.Stderr)
	rebuild()
}

// newHandler builds a slog handler using LOG_FORMAT from the environment.
// json → slog.JSONHandler, anything else → slog.TextHandler.
func newHandler(w io.Writer) slog.Handler {
	opts := &slog.HandlerOptions{Level: &levelVar}
	if strings.EqualFold(os.Getenv("LOG_FORMAT"), "json") {
		return slog.NewJSONHandler(w, opts)
	}
	return slog.NewTextHandler(w, opts)
}

func setOutput(w io.Writer) {
	output.Store(&w)
}

func currentOutput() io.Writer {
	if w := output.Load(); w != nil {
		return *w
	}
	return os.Stderr
}

func rebuild() {
	active.Store(slog.New(newHandler(currentOutput())))
}

func get() *slog.Logger {
	if l := active.Load(); l != nil {
		return l
	}
	return slog.Default()
}

// SetLevel sets the current logging level. Safe for concurrent use.
func SetLevel(level Level) {
	levelVar.Set(toSlogLevel(level))
}

// SetOutput redirects all logger output to w and rebuilds the handler
// against the current LOG_FORMAT. Intended for tests.
func SetOutput(w io.Writer) {
	setOutput(w)
	rebuild()
}

// Reinit rebuilds the package logger from the current environment.
// Call after flipping LOG_FORMAT in tests.
func Reinit() {
	rebuild()
}

// With returns a sub-logger carrying the supplied key/value attributes.
// Use at per-source or per-article sites to attach correlation fields.
func With(args ...any) *slog.Logger {
	return get().With(args...)
}

// Debugf logs a formatted message at DEBUG level.
func Debugf(format string, args ...any) {
	logf(slog.LevelDebug, format, args...)
}

// Infof logs a formatted message at INFO level.
func Infof(format string, args ...any) {
	logf(slog.LevelInfo, format, args...)
}

// Warnf logs a formatted message at WARN level.
func Warnf(format string, args ...any) {
	logf(slog.LevelWarn, format, args...)
}

// Errorf logs a formatted message at ERROR level.
func Errorf(format string, args ...any) {
	logf(slog.LevelError, format, args...)
}

// Fatalf logs at ERROR level (slog has no fatal) and exits the process.
func Fatalf(format string, args ...any) {
	l := get()
	l.Log(context.Background(), slog.LevelError, fmt.Sprintf(format, args...))
	os.Exit(1)
}

func logf(level slog.Level, format string, args ...any) {
	l := get()
	if !l.Enabled(context.Background(), level) {
		return
	}
	l.Log(context.Background(), level, fmt.Sprintf(format, args...))
}

// parseLevel converts a string level name to a Level value.
// Defaults to LevelInfo for unrecognized or empty values.
func parseLevel(s string) Level {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "DEBUG":
		return LevelDebug
	case "INFO":
		return LevelInfo
	case "WARN":
		return LevelWarn
	case "ERROR":
		return LevelError
	case "FATAL":
		return LevelFatal
	default:
		return LevelInfo
	}
}

func toSlogLevel(l Level) slog.Level {
	switch l {
	case LevelDebug:
		return slog.LevelDebug
	case LevelInfo:
		return slog.LevelInfo
	case LevelWarn:
		return slog.LevelWarn
	case LevelError, LevelFatal:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
