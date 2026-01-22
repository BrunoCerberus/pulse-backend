package models

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// Source represents an RSS feed source from the database
type Source struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Slug        string     `json:"slug"`
	FeedURL     string     `json:"feed_url"`
	WebsiteURL  *string    `json:"website_url"`
	LogoURL     *string    `json:"logo_url"`
	CategoryID  *string    `json:"category_id"`
	IsActive    bool       `json:"is_active"`
	LastFetched *time.Time `json:"last_fetched_at"`
}

// Article represents a news article to be stored
type Article struct {
	ID           string    `json:"id,omitempty"`
	Title        string    `json:"title"`
	Summary      *string   `json:"summary"`
	Content      *string   `json:"content"`
	URL          string    `json:"url"`
	URLHash      string    `json:"url_hash"`
	ImageURL     *string   `json:"image_url"`
	ThumbnailURL *string   `json:"thumbnail_url"`
	Author       *string   `json:"author"`
	SourceID     string    `json:"source_id"`
	CategoryID   *string   `json:"category_id"`
	PublishedAt  time.Time `json:"published_at"`
}

// NewArticle creates a new Article with computed URL hash
func NewArticle(title, url, sourceID string, categoryID *string, publishedAt time.Time) *Article {
	return &Article{
		Title:       title,
		URL:         url,
		URLHash:     HashURL(url),
		SourceID:    sourceID,
		CategoryID:  categoryID,
		PublishedAt: publishedAt,
	}
}

// HashURL generates a SHA256 hash of the URL for deduplication
func HashURL(url string) string {
	hash := sha256.Sum256([]byte(url))
	return hex.EncodeToString(hash[:])
}

// Category represents a news category
type Category struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	DisplayOrder int    `json:"display_order"`
}

// FetchLog tracks the status of a fetch operation
type FetchLog struct {
	ID                string     `json:"id,omitempty"`
	StartedAt         time.Time  `json:"started_at"`
	CompletedAt       *time.Time `json:"completed_at"`
	SourcesProcessed  int        `json:"sources_processed"`
	ArticlesFetched   int        `json:"articles_fetched"`
	ArticlesInserted  int        `json:"articles_inserted"`
	ArticlesSkipped   int        `json:"articles_skipped"`
	Errors            []string   `json:"errors"`
	Status            string     `json:"status"` // running, completed, failed
}

// FetchResult holds the result of fetching a single source
type FetchResult struct {
	Source          Source
	ArticlesFetched int
	ArticlesInserted int
	ArticlesSkipped  int
	Error           error
}
