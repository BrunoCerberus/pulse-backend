package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all configuration for the RSS worker
type Config struct {
	SupabaseURL          string
	SupabaseKey          string // service_role key for write access
	MaxConcurrent        int
	ArticleRetentionDays int

	// HostRateLimitRPS is the per-host requests/second limit applied to RSS,
	// og:image, and content HTTP clients. Supabase traffic is not throttled.
	HostRateLimitRPS float64
	// HostRateLimitBurst is the per-host burst allowance on top of the RPS.
	HostRateLimitBurst int

	// BackfillMaxAttempts caps how many times a single article can be retried
	// by backfill-images or backfill-content before it's excluded.
	BackfillMaxAttempts int
	// BackfillCooldownHours is the minimum gap between two backfill attempts
	// on the same article, preventing tight-loop retries within a run window.
	BackfillCooldownHours int

	// CircuitFailureThreshold is the number of consecutive fetch failures
	// before a source's circuit trips open. Values below this trigger only
	// retry-next-run; at/above, circuit_open_until is set to a cool-off window.
	CircuitFailureThreshold int
	// CircuitBaseBackoffHours is the initial cool-off window once the circuit
	// trips. Subsequent failures double the window (2^(failures-threshold)).
	CircuitBaseBackoffHours int
	// CircuitMaxBackoffHours caps the exponential backoff so a permanently dead
	// source still gets retried daily rather than every fortnight.
	CircuitMaxBackoffHours int
}

// Load reads configuration from environment variables
func Load() (*Config, error) {
	supabaseURL := os.Getenv("SUPABASE_URL")
	if supabaseURL == "" {
		return nil, fmt.Errorf("SUPABASE_URL environment variable is required")
	}

	supabaseKey := os.Getenv("SUPABASE_SERVICE_ROLE_KEY")
	if supabaseKey == "" {
		return nil, fmt.Errorf("SUPABASE_SERVICE_ROLE_KEY environment variable is required")
	}

	return &Config{
		SupabaseURL:           supabaseURL,
		SupabaseKey:           supabaseKey,
		MaxConcurrent:         5,
		ArticleRetentionDays:  30,
		HostRateLimitRPS:      envFloat("HOST_RATE_LIMIT_RPS", 2.0),
		HostRateLimitBurst:    envInt("HOST_RATE_LIMIT_BURST", 5),
		BackfillMaxAttempts:     envInt("BACKFILL_MAX_ATTEMPTS", 3),
		BackfillCooldownHours:   envInt("BACKFILL_COOLDOWN_HOURS", 24),
		CircuitFailureThreshold: envInt("CIRCUIT_FAILURE_THRESHOLD", 5),
		CircuitBaseBackoffHours: envInt("CIRCUIT_BASE_BACKOFF_HOURS", 1),
		CircuitMaxBackoffHours:  envInt("CIRCUIT_MAX_BACKOFF_HOURS", 24),
	}, nil
}

// envFloat returns the env var as float64, falling back to def on unset/invalid.
func envFloat(key string, def float64) float64 {
	if s := os.Getenv(key); s != "" {
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			return v
		}
	}
	return def
}

// envInt returns the env var as int, falling back to def on unset/invalid.
func envInt(key string, def int) int {
	if s := os.Getenv(key); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			return v
		}
	}
	return def
}
