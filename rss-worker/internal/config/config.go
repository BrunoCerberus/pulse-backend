package config

import (
	"fmt"
	"os"
)

// Config holds all configuration for the RSS worker
type Config struct {
	SupabaseURL    string
	SupabaseKey    string // service_role key for write access
	MaxConcurrent  int
	ArticleRetentionDays int
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
		SupabaseURL:          supabaseURL,
		SupabaseKey:          supabaseKey,
		MaxConcurrent:        5,
		ArticleRetentionDays: 30,
	}, nil
}
