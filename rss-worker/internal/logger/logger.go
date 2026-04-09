package logger

import (
	"log"
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

var currentLevel atomic.Int32

func init() {
	// #nosec G115 -- Level is a bounded enum (0..4); int→int32 cannot overflow.
	currentLevel.Store(int32(parseLevel(os.Getenv("LOG_LEVEL"))))
}

// SetLevel sets the current logging level. Safe for concurrent use.
func SetLevel(level Level) {
	// #nosec G115 -- Level is a bounded enum (0..4); int→int32 cannot overflow.
	currentLevel.Store(int32(level))
}

// Debugf logs a message at DEBUG level.
func Debugf(format string, args ...interface{}) {
	if Level(currentLevel.Load()) <= LevelDebug {
		log.Printf("[DEBUG] "+format, args...)
	}
}

// Infof logs a message at INFO level.
func Infof(format string, args ...interface{}) {
	if Level(currentLevel.Load()) <= LevelInfo {
		log.Printf("[INFO] "+format, args...)
	}
}

// Warnf logs a message at WARN level.
func Warnf(format string, args ...interface{}) {
	if Level(currentLevel.Load()) <= LevelWarn {
		log.Printf("[WARN] "+format, args...)
	}
}

// Errorf logs a message at ERROR level.
func Errorf(format string, args ...interface{}) {
	if Level(currentLevel.Load()) <= LevelError {
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
