package config

import (
	"os"
	"testing"
)

func TestLoad_Success(t *testing.T) {
	// Save original env vars
	origURL := os.Getenv("SUPABASE_URL")
	origKey := os.Getenv("SUPABASE_SERVICE_ROLE_KEY")
	defer func() {
		os.Setenv("SUPABASE_URL", origURL)
		os.Setenv("SUPABASE_SERVICE_ROLE_KEY", origKey)
	}()

	// Set test values
	os.Setenv("SUPABASE_URL", "https://test.supabase.co")
	os.Setenv("SUPABASE_SERVICE_ROLE_KEY", "test-service-role-key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if cfg.SupabaseURL != "https://test.supabase.co" {
		t.Errorf("SupabaseURL = %q, want %q", cfg.SupabaseURL, "https://test.supabase.co")
	}

	if cfg.SupabaseKey != "test-service-role-key" {
		t.Errorf("SupabaseKey = %q, want %q", cfg.SupabaseKey, "test-service-role-key")
	}
}

func TestLoad_DefaultValues(t *testing.T) {
	// Save original env vars
	origURL := os.Getenv("SUPABASE_URL")
	origKey := os.Getenv("SUPABASE_SERVICE_ROLE_KEY")
	defer func() {
		os.Setenv("SUPABASE_URL", origURL)
		os.Setenv("SUPABASE_SERVICE_ROLE_KEY", origKey)
	}()

	os.Setenv("SUPABASE_URL", "https://test.supabase.co")
	os.Setenv("SUPABASE_SERVICE_ROLE_KEY", "test-key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.MaxConcurrent != 5 {
		t.Errorf("MaxConcurrent = %d, want 5", cfg.MaxConcurrent)
	}

	if cfg.ArticleRetentionDays != 30 {
		t.Errorf("ArticleRetentionDays = %d, want 30", cfg.ArticleRetentionDays)
	}
}

func TestLoad_MissingSupabaseURL(t *testing.T) {
	// Save original env vars
	origURL := os.Getenv("SUPABASE_URL")
	origKey := os.Getenv("SUPABASE_SERVICE_ROLE_KEY")
	defer func() {
		os.Setenv("SUPABASE_URL", origURL)
		os.Setenv("SUPABASE_SERVICE_ROLE_KEY", origKey)
	}()

	os.Unsetenv("SUPABASE_URL")
	os.Setenv("SUPABASE_SERVICE_ROLE_KEY", "test-key")

	cfg, err := Load()
	if err == nil {
		t.Error("Load() should return error when SUPABASE_URL is missing")
	}

	if cfg != nil {
		t.Error("Load() should return nil config when error occurs")
	}
}

func TestLoad_MissingSupabaseKey(t *testing.T) {
	// Save original env vars
	origURL := os.Getenv("SUPABASE_URL")
	origKey := os.Getenv("SUPABASE_SERVICE_ROLE_KEY")
	defer func() {
		os.Setenv("SUPABASE_URL", origURL)
		os.Setenv("SUPABASE_SERVICE_ROLE_KEY", origKey)
	}()

	os.Setenv("SUPABASE_URL", "https://test.supabase.co")
	os.Unsetenv("SUPABASE_SERVICE_ROLE_KEY")

	cfg, err := Load()
	if err == nil {
		t.Error("Load() should return error when SUPABASE_SERVICE_ROLE_KEY is missing")
	}

	if cfg != nil {
		t.Error("Load() should return nil config when error occurs")
	}
}

func TestLoad_BothMissing(t *testing.T) {
	// Save original env vars
	origURL := os.Getenv("SUPABASE_URL")
	origKey := os.Getenv("SUPABASE_SERVICE_ROLE_KEY")
	defer func() {
		os.Setenv("SUPABASE_URL", origURL)
		os.Setenv("SUPABASE_SERVICE_ROLE_KEY", origKey)
	}()

	os.Unsetenv("SUPABASE_URL")
	os.Unsetenv("SUPABASE_SERVICE_ROLE_KEY")

	cfg, err := Load()
	if err == nil {
		t.Error("Load() should return error when both env vars are missing")
	}

	if cfg != nil {
		t.Error("Load() should return nil config when error occurs")
	}
}

func TestLoad_EmptySupabaseURL(t *testing.T) {
	// Save original env vars
	origURL := os.Getenv("SUPABASE_URL")
	origKey := os.Getenv("SUPABASE_SERVICE_ROLE_KEY")
	defer func() {
		os.Setenv("SUPABASE_URL", origURL)
		os.Setenv("SUPABASE_SERVICE_ROLE_KEY", origKey)
	}()

	os.Setenv("SUPABASE_URL", "")
	os.Setenv("SUPABASE_SERVICE_ROLE_KEY", "test-key")

	cfg, err := Load()
	if err == nil {
		t.Error("Load() should return error when SUPABASE_URL is empty string")
	}

	if cfg != nil {
		t.Error("Load() should return nil config when error occurs")
	}
}

func TestLoad_EmptySupabaseKey(t *testing.T) {
	// Save original env vars
	origURL := os.Getenv("SUPABASE_URL")
	origKey := os.Getenv("SUPABASE_SERVICE_ROLE_KEY")
	defer func() {
		os.Setenv("SUPABASE_URL", origURL)
		os.Setenv("SUPABASE_SERVICE_ROLE_KEY", origKey)
	}()

	os.Setenv("SUPABASE_URL", "https://test.supabase.co")
	os.Setenv("SUPABASE_SERVICE_ROLE_KEY", "")

	cfg, err := Load()
	if err == nil {
		t.Error("Load() should return error when SUPABASE_SERVICE_ROLE_KEY is empty string")
	}

	if cfg != nil {
		t.Error("Load() should return nil config when error occurs")
	}
}
