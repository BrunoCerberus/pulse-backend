// Package database provides a client for interacting with the Supabase REST API.
//
// The client handles all database operations including:
//   - Fetching RSS sources
//   - Inserting and updating articles with deduplication via URL hash
//   - Managing fetch logs for monitoring
//   - Cleanup of old articles
//   - Backfill operations for images and content
//
// All operations use the Supabase REST API directly via HTTP, authenticated
// with the service role key for full read/write access.
package database

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/pulsefeed/rss-worker/internal/config"
	"github.com/pulsefeed/rss-worker/internal/models"
)

// Client handles all Supabase database operations via the REST API.
// It maintains an HTTP client with a 30-second timeout for all requests.
type Client struct {
	baseURL    string       // Supabase REST API base URL (e.g., https://xxx.supabase.co/rest/v1)
	apiKey     string       // Supabase service role key for authentication
	httpClient *http.Client // HTTP client with configured timeout
}

// NewClient creates a new Supabase client
func NewClient(cfg *config.Config) *Client {
	return &Client{
		baseURL: cfg.SupabaseURL + "/rest/v1",
		apiKey:  cfg.SupabaseKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GetActiveSources retrieves all active RSS sources
func (c *Client) GetActiveSources() ([]models.Source, error) {
	url := fmt.Sprintf("%s/sources?is_active=eq.true&select=*", c.baseURL)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get sources: %s - %s", resp.Status, string(body))
	}

	var sources []models.Source
	if err := json.NewDecoder(resp.Body).Decode(&sources); err != nil {
		return nil, err
	}

	return sources, nil
}

// InsertArticle inserts a new article or updates image_url if it already exists
// Returns true if inserted (new), false if updated/skipped (existing)
func (c *Client) InsertArticle(article *models.Article) (bool, error) {
	url := fmt.Sprintf("%s/articles", c.baseURL)

	data, err := json.Marshal(article)
	if err != nil {
		return false, err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		return false, err
	}
	c.setHeaders(req)
	req.Header.Set("Prefer", "return=minimal")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	// 201 = created (new article)
	if resp.StatusCode == http.StatusCreated {
		return true, nil
	}

	// 409 = conflict (duplicate url_hash) - try to update image_url if we have an og:image
	if resp.StatusCode == http.StatusConflict {
		// Only update if we have a real og:image (different from thumbnail)
		hasOGImage := article.ImageURL != nil && *article.ImageURL != "" &&
			(article.ThumbnailURL == nil || *article.ImageURL != *article.ThumbnailURL)

		if hasOGImage {
			fmt.Printf("[DB] Updating image_url for existing article: %s\n", article.URL)
			if err := c.UpdateArticleImage(article.URLHash, *article.ImageURL); err != nil {
				fmt.Printf("[DB] Failed to update image: %v\n", err)
			}
		}
		return false, nil
	}

	body, _ := io.ReadAll(resp.Body)
	return false, fmt.Errorf("failed to insert article: %s - %s", resp.Status, string(body))
}

// UpdateArticleImage updates just the image_url for an existing article
func (c *Client) UpdateArticleImage(urlHash string, imageURL string) error {
	url := fmt.Sprintf("%s/articles?url_hash=eq.%s", c.baseURL, urlHash)

	data := map[string]interface{}{
		"image_url": imageURL,
	}

	body, err := json.Marshal(data)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("PATCH", url, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to update article image: %s - %s", resp.Status, string(respBody))
	}

	return nil
}

// InsertArticles inserts multiple articles in batch
func (c *Client) InsertArticles(articles []*models.Article) (inserted int, skipped int, err error) {
	for _, article := range articles {
		ok, insertErr := c.InsertArticle(article)
		if insertErr != nil {
			// Log error but continue with other articles
			fmt.Printf("Error inserting article %s: %v\n", article.URL, insertErr)
			continue
		}
		if ok {
			inserted++
		} else {
			skipped++
		}
	}
	return inserted, skipped, nil
}

// UpdateSourceLastFetched updates the last_fetched_at timestamp for a source
func (c *Client) UpdateSourceLastFetched(sourceID string) error {
	url := fmt.Sprintf("%s/sources?id=eq.%s", c.baseURL, sourceID)

	data := map[string]interface{}{
		"last_fetched_at": time.Now().UTC().Format(time.RFC3339),
		"updated_at":      time.Now().UTC().Format(time.RFC3339),
	}

	body, err := json.Marshal(data)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("PATCH", url, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to update source: %s - %s", resp.Status, string(respBody))
	}

	return nil
}

// CreateFetchLog creates a new fetch log entry
func (c *Client) CreateFetchLog() (*models.FetchLog, error) {
	url := fmt.Sprintf("%s/fetch_logs", c.baseURL)

	log := &models.FetchLog{
		StartedAt: time.Now().UTC(),
		Status:    "running",
		Errors:    []string{},
	}

	data, err := json.Marshal(log)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	req.Header.Set("Prefer", "return=representation")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to create fetch log: %s - %s", resp.Status, string(body))
	}

	var logs []models.FetchLog
	if err := json.NewDecoder(resp.Body).Decode(&logs); err != nil {
		return nil, err
	}

	if len(logs) == 0 {
		return nil, fmt.Errorf("no fetch log returned")
	}

	return &logs[0], nil
}

// UpdateFetchLog updates a fetch log with final results
func (c *Client) UpdateFetchLog(log *models.FetchLog) error {
	url := fmt.Sprintf("%s/fetch_logs?id=eq.%s", c.baseURL, log.ID)

	now := time.Now().UTC()
	log.CompletedAt = &now

	data, err := json.Marshal(log)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("PATCH", url, bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to update fetch log: %s - %s", resp.Status, string(body))
	}

	return nil
}

// CleanupOldArticles removes articles older than the specified days
func (c *Client) CleanupOldArticles(daysToKeep int) (int, error) {
	// Call the PostgreSQL function we created
	url := fmt.Sprintf("%s/rpc/cleanup_old_articles", c.baseURL)

	data := map[string]int{"days_to_keep": daysToKeep}
	body, err := json.Marshal(data)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return 0, err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("failed to cleanup articles: %s - %s", resp.Status, string(respBody))
	}

	var count int
	if err := json.NewDecoder(resp.Body).Decode(&count); err != nil {
		return 0, err
	}

	return count, nil
}

// setHeaders sets the required Supabase headers
func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("apikey", c.apiKey)
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
}

// ArticleForBackfill represents minimal article data needed for og:image backfill.
// Only the fields required for fetching and comparing images are included
// to minimize data transfer from the database.
type ArticleForBackfill struct {
	URLHash      string  `json:"url_hash"`      // SHA256 hash of URL, used as unique identifier
	URL          string  `json:"url"`           // Original article URL to fetch og:image from
	ImageURL     *string `json:"image_url"`     // Current image URL (may be low-res or nil)
	ThumbnailURL *string `json:"thumbnail_url"` // Original RSS thumbnail for comparison
}

// ArticleForContentBackfill represents minimal article data needed for content extraction.
// Only the fields required for fetching article content are included.
type ArticleForContentBackfill struct {
	URLHash string  `json:"url_hash"` // SHA256 hash of URL, used as unique identifier
	URL     string  `json:"url"`      // Original article URL to extract content from
	Content *string `json:"content"`  // Current content (nil or empty for backfill candidates)
}

// GetArticlesNeedingOGImage retrieves articles that need og:image backfill
// (where image_url equals thumbnail_url, is null, or contains low-res indicators)
func (c *Client) GetArticlesNeedingOGImage(limit int) ([]ArticleForBackfill, error) {
	// Get articles where:
	// - image_url is null, OR
	// - image_url equals thumbnail_url, OR
	// - image_url contains width=140 (Guardian low-res)
	url := fmt.Sprintf("%s/articles?select=url_hash,url,image_url,thumbnail_url&or=(image_url.is.null,image_url.eq.thumbnail_url,image_url.like.*width=140*)&limit=%d", c.baseURL, limit)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get articles: %s - %s", resp.Status, string(body))
	}

	var articles []ArticleForBackfill
	if err := json.NewDecoder(resp.Body).Decode(&articles); err != nil {
		return nil, err
	}

	return articles, nil
}

// GetArticlesNeedingContent retrieves articles that need content backfill
// (where content is null or empty)
func (c *Client) GetArticlesNeedingContent(limit int) ([]ArticleForContentBackfill, error) {
	url := fmt.Sprintf("%s/articles?select=url_hash,url,content&or=(content.is.null,content.eq.)&limit=%d", c.baseURL, limit)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get articles: %s - %s", resp.Status, string(body))
	}

	var articles []ArticleForContentBackfill
	if err := json.NewDecoder(resp.Body).Decode(&articles); err != nil {
		return nil, err
	}

	return articles, nil
}

// UpdateArticleContent updates the content field for an existing article
func (c *Client) UpdateArticleContent(urlHash string, content string) error {
	url := fmt.Sprintf("%s/articles?url_hash=eq.%s", c.baseURL, urlHash)

	data := map[string]interface{}{
		"content": content,
	}

	body, err := json.Marshal(data)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("PATCH", url, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to update article content: %s - %s", resp.Status, string(respBody))
	}

	return nil
}
