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
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/pulsefeed/rss-worker/internal/config"
	"github.com/pulsefeed/rss-worker/internal/httputil"
	"github.com/pulsefeed/rss-worker/internal/logger"
	"github.com/pulsefeed/rss-worker/internal/models"
)

const (
	maxRetries    = 3
	retryBaseWait = 500 * time.Millisecond
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
		baseURL:    cfg.SupabaseURL + "/rest/v1",
		apiKey:     cfg.SupabaseKey,
		httpClient: httputil.NewClient(30 * time.Second),
	}
}

// GetActiveSources retrieves all active RSS sources with embedded category info.
// Uses PostgREST embedding to include category name and slug.
// Filters out sources that aren't due for fetching based on their fetch_interval_hours.
// Also skips sources whose circuit breaker is open (migration 019): the PostgREST
// `or=(circuit_open_until.is.null,circuit_open_until.lt.{now})` filter keeps only
// sources with no trip or whose cool-off window has elapsed.
func (c *Client) GetActiveSources() ([]models.Source, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	url := fmt.Sprintf(
		"%s/sources?is_active=eq.true&select=*,categories(name,slug)&or=(circuit_open_until.is.null,circuit_open_until.lt.%s)",
		c.baseURL, now,
	)

	resp, err := c.doWithRetry("GET", url, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get sources: %s - %s", resp.Status, readErrorBody(resp))
	}

	var allSources []models.Source
	if err := json.NewDecoder(resp.Body).Decode(&allSources); err != nil {
		return nil, err
	}

	// Filter to sources due for fetching
	sources := make([]models.Source, 0, len(allSources))
	for _, s := range allSources {
		if s.ShouldFetch() {
			sources = append(sources, s)
		}
	}

	return sources, nil
}

// ImageUpdate holds a url_hash and image_url pair for batch image updates.
type ImageUpdate struct {
	URLHash  string `json:"url_hash"`
	ImageURL string `json:"image_url"`
}

const batchSize = 50

// InsertArticles inserts multiple articles in batches via POST with JSON arrays.
// Uses PostgREST's on_conflict=url_hash with ignore-duplicates to handle deduplication.
// After inserting, batch-updates image_url for any articles where og:image differs from thumbnail.
func (c *Client) InsertArticles(articles []*models.Article) (inserted int, skipped int, err error) {
	if len(articles) == 0 {
		return 0, 0, nil
	}

	// Build a set of all url_hashes being inserted
	allHashes := make(map[string]*models.Article, len(articles))
	for _, a := range articles {
		allHashes[a.URLHash] = a
	}

	// Insert in batches of batchSize
	insertedHashes := make(map[string]struct{})
	var batchErrors []error
	for i := 0; i < len(articles); i += batchSize {
		end := i + batchSize
		if end > len(articles) {
			end = len(articles)
		}
		batch := articles[i:end]

		hashes, batchErr := c.insertArticleBatch(batch)
		if batchErr != nil {
			logger.Errorf("[DB] Error inserting batch of %d articles: %v", len(batch), batchErr)
			batchErrors = append(batchErrors, fmt.Errorf("batch %d-%d: %w", i, end, batchErr))
			continue
		}
		for _, h := range hashes {
			insertedHashes[h] = struct{}{}
		}
	}

	inserted = len(insertedHashes)
	skipped = len(articles) - inserted

	// Batch-update image_url for skipped articles that have a better og:image
	var imageUpdates []ImageUpdate
	for _, article := range articles {
		if _, wasInserted := insertedHashes[article.URLHash]; wasInserted {
			continue
		}
		hasOGImage := article.ImageURL != nil && *article.ImageURL != "" &&
			(article.ThumbnailURL == nil || *article.ImageURL != *article.ThumbnailURL)
		if hasOGImage {
			imageUpdates = append(imageUpdates, ImageUpdate{
				URLHash:  article.URLHash,
				ImageURL: *article.ImageURL,
			})
		}
	}

	if len(imageUpdates) > 0 {
		if updateErr := c.BatchUpdateArticleImages(imageUpdates); updateErr != nil {
			logger.Warnf("[DB] Failed to batch-update images: %v", updateErr)
		}
	}

	return inserted, skipped, errors.Join(batchErrors...)
}

// insertArticleBatch inserts a batch of articles and returns the url_hashes of newly inserted rows.
func (c *Client) insertArticleBatch(batch []*models.Article) ([]string, error) {
	url := fmt.Sprintf("%s/articles?on_conflict=url_hash&select=url_hash", c.baseURL)

	data, err := json.Marshal(batch)
	if err != nil {
		return nil, err
	}

	resp, err := c.doWithRetry("POST", url, data, map[string]string{
		"Prefer": "resolution=ignore-duplicates,return=representation",
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("batch insert failed: %s - %s", resp.Status, readErrorBody(resp))
	}

	var inserted []struct {
		URLHash string `json:"url_hash"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&inserted); err != nil {
		return nil, fmt.Errorf("failed to decode batch response: %w", err)
	}

	hashes := make([]string, len(inserted))
	for i, row := range inserted {
		hashes[i] = row.URLHash
	}
	return hashes, nil
}

// BatchUpdateArticleImages updates image_url for multiple articles via RPC.
func (c *Client) BatchUpdateArticleImages(updates []ImageUpdate) error {
	if len(updates) == 0 {
		return nil
	}

	url := fmt.Sprintf("%s/rpc/batch_update_article_images", c.baseURL)

	payload := map[string]interface{}{
		"updates": updates,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := c.doWithRetry("POST", url, data)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("batch image update failed: %s - %s", resp.Status, readErrorBody(resp))
	}

	return nil
}

// UpdateArticleImage updates just the image_url for an existing article.
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
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to update article image: %s - %s", resp.Status, readErrorBody(resp))
	}

	return nil
}

// SourceFetchState is the per-source state persisted after each fetch cycle.
// Passed to BatchUpdateSourceFetchState so one RPC call can record different
// outcomes across sources: success with fresh ETag, success-304 with preserved
// ETag, or failure with incremented counter and possible circuit trip.
//
// Nil pointer fields serialize to JSON null, which jsonb_to_recordset interprets
// as NULL in the UPDATE — this is how a successful fetch with no ETag header
// (or a circuit that should close) clears the column.
type SourceFetchState struct {
	ID                  string     `json:"id"`
	ETag                *string    `json:"etag"`
	LastModified        *string    `json:"last_modified"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	CircuitOpenUntil    *time.Time `json:"circuit_open_until"`
	LastFetchedAt       *time.Time `json:"last_fetched_at"`
}

// BatchUpdateSourceFetchState calls the batch_update_source_fetch_state RPC
// (migration 020) to persist per-source state — cache validators, failure
// counter, and circuit cool-off — in a single round-trip.
func (c *Client) BatchUpdateSourceFetchState(updates []SourceFetchState) error {
	if len(updates) == 0 {
		return nil
	}

	url := fmt.Sprintf("%s/rpc/batch_update_source_fetch_state", c.baseURL)

	payload := map[string]interface{}{
		"updates": updates,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := c.doWithRetry("POST", url, data)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("batch update source fetch state failed: %s - %s", resp.Status, readErrorBody(resp))
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
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("failed to create fetch log: %s - %s", resp.Status, readErrorBody(resp))
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
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to update fetch log: %s - %s", resp.Status, readErrorBody(resp))
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
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("failed to cleanup articles: %s - %s", resp.Status, readErrorBody(resp))
	}

	var count int
	if err := json.NewDecoder(resp.Body).Decode(&count); err != nil {
		return 0, err
	}

	return count, nil
}

// isRetryable returns true for HTTP status codes that indicate a transient error.
func isRetryable(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests ||
		statusCode == http.StatusBadGateway ||
		statusCode == http.StatusServiceUnavailable ||
		statusCode == http.StatusGatewayTimeout
}

// doWithRetry executes an HTTP request with exponential backoff retry for transient errors.
// Retries up to maxRetries times on 429/502/503/504 with 500ms, 1s, 2s delays.
// Optional extraHeaders are applied to each request attempt.
func (c *Client) doWithRetry(method, url string, body []byte, extraHeaders ...map[string]string) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		var reqBody io.Reader
		if body != nil {
			reqBody = bytes.NewReader(body)
		}

		req, err := http.NewRequest(method, url, reqBody)
		if err != nil {
			return nil, err
		}
		c.setHeaders(req)
		for _, headers := range extraHeaders {
			for k, v := range headers {
				req.Header.Set(k, v)
			}
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			if attempt < maxRetries {
				time.Sleep(retryBaseWait * time.Duration(1<<uint(attempt)))
			}
			continue
		}

		if !isRetryable(resp.StatusCode) {
			return resp, nil
		}

		// Drain and close body before retrying
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		lastErr = fmt.Errorf("retryable status: %s", resp.Status)

		if attempt < maxRetries {
			wait := retryBaseWait * time.Duration(1<<uint(attempt))
			logger.Warnf("[DB] Retryable error %d on %s %s, retrying in %v", resp.StatusCode, method, url, wait)
			time.Sleep(wait)
		}
	}
	return nil, fmt.Errorf("request failed after %d retries: %w", maxRetries, lastErr)
}

// CleanupOldFetchLogs removes fetch log entries older than the specified days.
// Uses Supabase REST API DELETE with date filter.
func (c *Client) CleanupOldFetchLogs(daysToKeep int) (int, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -daysToKeep).Format(time.RFC3339)
	url := fmt.Sprintf("%s/fetch_logs?started_at=lt.%s", c.baseURL, cutoff)

	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return 0, err
	}
	c.setHeaders(req)
	req.Header.Set("Prefer", "return=representation")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("failed to cleanup fetch logs: %s - %s", resp.Status, readErrorBody(resp))
	}

	var deleted []json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&deleted); err != nil {
		return 0, err
	}

	return len(deleted), nil
}

// setHeaders sets the required Supabase headers
func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("apikey", c.apiKey)
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
}

// readErrorBody reads and returns the response body as a string for error reporting.
// It silently handles read errors since this is only used in error paths.
func readErrorBody(resp *http.Response) string {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return string(body)
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
// (where image_url equals thumbnail_url, is null, or contains low-res indicators).
// Excludes articles whose attempt counter is at maxAttempts or whose last attempt
// was within cooldownHours — this prevents the worker from re-fetching dead URLs
// on every cron tick (see migration 018).
func (c *Client) GetArticlesNeedingOGImage(limit, maxAttempts, cooldownHours int) ([]ArticleForBackfill, error) {
	cutoff := time.Now().UTC().Add(-time.Duration(cooldownHours) * time.Hour).Format(time.RFC3339)
	filter := fmt.Sprintf(
		"and=(or(image_url.is.null,image_url.eq.thumbnail_url,image_url.like.*width=140*),image_backfill_attempts.lt.%d,or(image_backfill_last_attempt_at.is.null,image_backfill_last_attempt_at.lt.%s))",
		maxAttempts, cutoff,
	)
	url := fmt.Sprintf("%s/articles?select=url_hash,url,image_url,thumbnail_url&%s&limit=%d", c.baseURL, filter, limit)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get articles: %s - %s", resp.Status, readErrorBody(resp))
	}

	var articles []ArticleForBackfill
	if err := json.NewDecoder(resp.Body).Decode(&articles); err != nil {
		return nil, err
	}

	return articles, nil
}

// GetArticlesNeedingContent retrieves articles that need content backfill
// (where content is null or empty). Excludes articles that exhausted maxAttempts
// or had an attempt within cooldownHours (see migration 018).
func (c *Client) GetArticlesNeedingContent(limit, maxAttempts, cooldownHours int) ([]ArticleForContentBackfill, error) {
	cutoff := time.Now().UTC().Add(-time.Duration(cooldownHours) * time.Hour).Format(time.RFC3339)
	filter := fmt.Sprintf(
		"and=(or(content.is.null,content.eq.),content_backfill_attempts.lt.%d,or(content_backfill_last_attempt_at.is.null,content_backfill_last_attempt_at.lt.%s))",
		maxAttempts, cutoff,
	)
	url := fmt.Sprintf("%s/articles?select=url_hash,url,content&%s&limit=%d", c.baseURL, filter, limit)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get articles: %s - %s", resp.Status, readErrorBody(resp))
	}

	var articles []ArticleForContentBackfill
	if err := json.NewDecoder(resp.Body).Decode(&articles); err != nil {
		return nil, err
	}

	return articles, nil
}

// BumpBackfillAttempts increments the attempt counter and stamps last_attempt_at
// for all given url_hashes via the bump_backfill_attempts RPC (migration 018).
// kind selects which column pair is updated: "image" or "content".
//
// Call this with hashes of articles whose backfill attempt failed so they get
// excluded from the next run until the cooldown elapses.
func (c *Client) BumpBackfillAttempts(urlHashes []string, kind string) error {
	if len(urlHashes) == 0 {
		return nil
	}

	url := fmt.Sprintf("%s/rpc/bump_backfill_attempts", c.baseURL)

	payload := map[string]interface{}{
		"url_hashes": urlHashes,
		"kind":       kind,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := c.doWithRetry("POST", url, data)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bump backfill attempts failed: %s - %s", resp.Status, readErrorBody(resp))
	}

	return nil
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
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to update article content: %s - %s", resp.Status, readErrorBody(resp))
	}

	return nil
}
