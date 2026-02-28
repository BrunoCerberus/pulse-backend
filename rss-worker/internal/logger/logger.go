package logger

import (
	"log"
	"os"
	"strings"
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

var currentLevel Level

func init() {
	currentLevel = parseLevel(os.Getenv("LOG_LEVEL"))
}

// SetLevel sets the current logging level. Useful for testing.
func SetLevel(level Level) {
	currentLevel = level
}

// Debugf logs a message at DEBUG level.
func Debugf(format string, args ...interface{}) {
	if currentLevel <= LevelDebug {
		log.Printf("[DEBUG] "+format, args...)
	}
}

// Infof logs a message at INFO level.
func Infof(format string, args ...interface{}) {
	if currentLevel <= LevelInfo {
		log.Printf("[INFO] "+format, args...)
	}
}

// Warnf logs a message at WARN level.
func Warnf(format string, args ...interface{}) {
	if currentLevel <= LevelWarn {
		log.Printf("[WARN] "+format, args...)
	}
}

// Errorf logs a message at ERROR level.
func Errorf(format string, args ...interface{}) {
	if currentLevel <= LevelError {
		log.Printf("[ERROR] "+format, args...)
	}
}

// Fatalf logs a message at FATAL level and exits the process.
func Fatalf(format string, args ...interface{}) {
	log.Fatalf("[FATAL] "+format, args...)
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
