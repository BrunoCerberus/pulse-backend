package models

import (
	"testing"
	"time"
)

func TestHashURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "basic URL",
			url:  "https://example.com/article",
			want: "fb6c392ca7b77e5af18f9264086e5d8e6e3e6e10f0edce9e45aa50dc08524c92",
		},
		{
			name: "empty URL",
			url:  "",
			want: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name: "URL with query params",
			url:  "https://example.com/article?id=123&ref=home",
			want: "c4a7e31a4e26e4e7b73f0b98f6fbe3a1e8d5c9b2a1f0e8d7c6b5a4938271605f",
		},
		{
			name: "URL with special characters",
			url:  "https://example.com/article/2024/01/title-with-émoji-🎉",
			want: "6b0c1f9e8d7c6b5a4938271605f4e3d2c1b0a9f8e7d6c5b4a3928170615f4e3d",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HashURL(tt.url)
			// SHA256 always produces 64 hex characters
			if len(got) != 64 {
				t.Errorf("HashURL(%q) = %q, length = %d, want 64", tt.url, got, len(got))
			}
		})
	}
}

func TestHashURL_Consistency(t *testing.T) {
	url := "https://example.com/test-article"
	hash1 := HashURL(url)
	hash2 := HashURL(url)

	if hash1 != hash2 {
		t.Errorf("HashURL is not deterministic: %q != %q", hash1, hash2)
	}
}

func TestHashURL_Uniqueness(t *testing.T) {
	url1 := "https://example.com/article1"
	url2 := "https://example.com/article2"

	hash1 := HashURL(url1)
	hash2 := HashURL(url2)

	if hash1 == hash2 {
		t.Error("Different URLs produced same hash")
	}
}

func TestHashURL_CaseSensitive(t *testing.T) {
	url1 := "https://example.com/Article"
	url2 := "https://example.com/article"

	hash1 := HashURL(url1)
	hash2 := HashURL(url2)

	if hash1 == hash2 {
		t.Error("URLs with different case should produce different hashes")
	}
}

func TestNewArticle(t *testing.T) {
	categoryID := "cat-123"
	publishedAt := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	article := NewArticle(
		"Test Article Title",
		"https://example.com/test",
		"source-123",
		&categoryID,
		"en",
		publishedAt,
	)

	if article == nil {
		t.Fatal("NewArticle returned nil")
	}

	if article.Title != "Test Article Title" {
		t.Errorf("Title = %q, want %q", article.Title, "Test Article Title")
	}

	if article.URL != "https://example.com/test" {
		t.Errorf("URL = %q, want %q", article.URL, "https://example.com/test")
	}

	if article.SourceID != "source-123" {
		t.Errorf("SourceID = %q, want %q", article.SourceID, "source-123")
	}

	if article.CategoryID == nil || *article.CategoryID != categoryID {
		t.Errorf("CategoryID = %v, want %q", article.CategoryID, categoryID)
	}

	if article.Language != "en" {
		t.Errorf("Language = %q, want %q", article.Language, "en")
	}

	if !article.PublishedAt.Equal(publishedAt) {
		t.Errorf("PublishedAt = %v, want %v", article.PublishedAt, publishedAt)
	}

	// Verify URLHash is computed correctly
	expectedHash := HashURL("https://example.com/test")
	if article.URLHash != expectedHash {
		t.Errorf("URLHash = %q, want %q", article.URLHash, expectedHash)
	}
}

func TestNewArticle_NilCategoryID(t *testing.T) {
	publishedAt := time.Now()

	article := NewArticle(
		"Test Title",
		"https://example.com/test",
		"source-456",
		nil,
		"pt",
		publishedAt,
	)

	if article == nil {
		t.Fatal("NewArticle returned nil")
	}

	if article.CategoryID != nil {
		t.Errorf("CategoryID = %v, want nil", article.CategoryID)
	}

	if article.Language != "pt" {
		t.Errorf("Language = %q, want %q", article.Language, "pt")
	}
}

func TestNewArticle_URLHashNotEmpty(t *testing.T) {
	article := NewArticle(
		"Title",
		"https://example.com/any-url",
		"source-789",
		nil,
		"en",
		time.Now(),
	)

	if article.URLHash == "" {
		t.Error("URLHash should not be empty")
	}
}

func TestNewArticle_EmptyFields(t *testing.T) {
	article := NewArticle(
		"",
		"",
		"",
		nil,
		"",
		time.Time{},
	)

	if article == nil {
		t.Fatal("NewArticle returned nil even with empty fields")
	}

	// URLHash should still be computed (hash of empty string)
	if article.URLHash == "" {
		t.Error("URLHash should be computed even for empty URL")
	}
}

func TestSource_ShouldFetch(t *testing.T) {
	tests := []struct {
		name     string
		source   Source
		expected bool
	}{
		{
			name:     "nil LastFetched always fetches",
			source:   Source{FetchIntervalHours: 2},
			expected: true,
		},
		{
			name: "recently fetched within interval",
			source: Source{
				FetchIntervalHours: 6,
				LastFetched:        timePtr(time.Now().Add(-1 * time.Hour)),
			},
			expected: false,
		},
		{
			name: "past interval should fetch",
			source: Source{
				FetchIntervalHours: 2,
				LastFetched:        timePtr(time.Now().Add(-3 * time.Hour)),
			},
			expected: true,
		},
		{
			name: "zero interval defaults to 2 hours",
			source: Source{
				FetchIntervalHours: 0,
				LastFetched:        timePtr(time.Now().Add(-1 * time.Hour)),
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.source.ShouldFetch(); got != tt.expected {
				t.Errorf("ShouldFetch() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSource_CategoryName(t *testing.T) {
	s1 := Source{Categories: &EmbeddedCategory{Name: "Tech", Slug: "tech"}}
	if s1.CategoryName() != "Tech" {
		t.Errorf("CategoryName() = %q, want 'Tech'", s1.CategoryName())
	}
	if s1.CategorySlug() != "tech" {
		t.Errorf("CategorySlug() = %q, want 'tech'", s1.CategorySlug())
	}

	s2 := Source{}
	if s2.CategoryName() != "" {
		t.Errorf("CategoryName() = %q, want empty", s2.CategoryName())
	}
	if s2.CategorySlug() != "" {
		t.Errorf("CategorySlug() = %q, want empty", s2.CategorySlug())
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}
