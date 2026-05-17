package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
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

	// ImagePruneDays is the age (in days) past which image_url and
	// thumbnail_url are nulled by the daily cleanup. Must be > 0 and
	// <= ArticleRetentionDays — nulling URLs on rows already past full
	// row-level deletion is a no-op and a sign of misconfiguration.
	// Shared between the prune RPC and the og:image backfill candidate
	// filter to prevent the two cutoffs from drifting.
	ImagePruneDays int
	// ContentPruneDays is the age (in days) past which articles.content is
	// nulled by the daily cleanup. Same bounds as ImagePruneDays. Shared
	// between the prune RPC and the content backfill candidate filter so
	// the worker doesn't re-extract what cleanup just nulled.
	ContentPruneDays int
}

// Load reads configuration from environment variables
func Load() (*Config, error) {
	supabaseURL := os.Getenv("SUPABASE_URL")
	if supabaseURL == "" {
		return nil, fmt.Errorf("SUPABASE_URL environment variable is required")
	}
	// Require https:// to refuse cleartext (HTTP) requests that would expose
	// the service-role key. A misconfigured typo-squat URL would otherwise
	// silently leak credentials. Loopback http:// is allowed for local dev
	// + tests (httptest.Server returns http://127.0.0.1:NNNN).
	if !strings.HasPrefix(supabaseURL, "https://") && !isLoopbackHTTP(supabaseURL) {
		return nil, fmt.Errorf("SUPABASE_URL must use https:// scheme, got: %q", supabaseURL)
	}

	supabaseKey := os.Getenv("SUPABASE_SERVICE_ROLE_KEY")
	if supabaseKey == "" {
		return nil, fmt.Errorf("SUPABASE_SERVICE_ROLE_KEY environment variable is required")
	}

	cfg := &Config{
		SupabaseURL:             supabaseURL,
		SupabaseKey:             supabaseKey,
		MaxConcurrent:           5,
		ArticleRetentionDays:    7,
		HostRateLimitRPS:        envFloat("HOST_RATE_LIMIT_RPS", 2.0),
		HostRateLimitBurst:      envInt("HOST_RATE_LIMIT_BURST", 5),
		BackfillMaxAttempts:     envInt("BACKFILL_MAX_ATTEMPTS", 3),
		BackfillCooldownHours:   envInt("BACKFILL_COOLDOWN_HOURS", 24),
		CircuitFailureThreshold: envInt("CIRCUIT_FAILURE_THRESHOLD", 5),
		CircuitBaseBackoffHours: envInt("CIRCUIT_BASE_BACKOFF_HOURS", 1),
		CircuitMaxBackoffHours:  envInt("CIRCUIT_MAX_BACKOFF_HOURS", 24),
		ImagePruneDays:          envInt("IMAGE_PRUNE_DAYS", 3),
		ContentPruneDays:        envInt("CONTENT_PRUNE_DAYS", 2),
	}

	if err := validatePruneDays("IMAGE_PRUNE_DAYS", cfg.ImagePruneDays, cfg.ArticleRetentionDays); err != nil {
		return nil, err
	}
	if err := validatePruneDays("CONTENT_PRUNE_DAYS", cfg.ContentPruneDays, cfg.ArticleRetentionDays); err != nil {
		return nil, err
	}

	return cfg, nil
}

// validatePruneDays fails fast on values that would either no-op the prune
// (>= retention, since the row gets fully deleted at retention) or invert it
// (<= 0, which would null every row). Catching these at startup is the
// cheapest place — by the time the cleanup RPC runs against production data,
// a typo is already destroying the wrong rows.
func validatePruneDays(name string, value, retentionDays int) error {
	if value <= 0 {
		return fmt.Errorf("%s must be > 0, got %d", name, value)
	}
	if value > retentionDays {
		return fmt.Errorf("%s (%d) must be <= ArticleRetentionDays (%d): nulling rows already past full deletion is a no-op", name, value, retentionDays)
	}
	return nil
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

// isLoopbackHTTP reports whether url is http:// pointing at loopback. Used to
// allow plaintext for local dev / test (httptest.Server). Production URLs
// should never match.
func isLoopbackHTTP(u string) bool {
	for _, prefix := range []string{
		"http://localhost/", "http://localhost:",
		"http://127.0.0.1/", "http://127.0.0.1:",
		"http://[::1]/", "http://[::1]:",
	} {
		if strings.HasPrefix(u, prefix) {
			return true
		}
	}
	return false
}
