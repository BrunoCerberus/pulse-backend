// Package main provides the entry point for the Pulse RSS Worker.
//
// The worker fetches RSS feeds from configured sources, parses articles,
// enriches them with og:image and content extraction, and stores them
// in Supabase. It supports several commands:
//
//   - Default: Fetch all active RSS feeds and insert new articles
//   - cleanup: Remove articles older than the retention period
//   - backfill-images: Fetch og:image for articles missing high-res images
//   - backfill-content: Extract full content for articles missing content
//
// Usage:
//
//	go run .                  # Run RSS fetch
//	go run . cleanup          # Clean old articles
//	go run . backfill-images  # Backfill missing images
//	go run . backfill-content # Backfill missing content
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/pulsefeed/rss-worker/internal/config"
	"github.com/pulsefeed/rss-worker/internal/database"
	"github.com/pulsefeed/rss-worker/internal/models"
	"github.com/pulsefeed/rss-worker/internal/parser"
)

// Named constants for timeouts, limits, and worker counts.
const (
	fetchTimeout           = 10 * time.Minute
	ogImageBackfillTimeout = 30 * time.Minute
	ogImageBackfillLimit   = 500
	ogImageBackfillWorkers = 5
	contentBackfillTimeout = 60 * time.Minute
	contentBackfillLimit   = 200
	contentBackfillWorkers = 3
)

// Store abstracts the database operations used by the worker commands.
// This allows testing with mock implementations and decouples main from
// the concrete database.Client type.
type Store interface {
	GetActiveSources() ([]models.Source, error)
	InsertArticles(articles []*models.Article) (inserted int, skipped int, err error)
	UpdateSourceLastFetched(sourceID string) error
	CreateFetchLog() (*models.FetchLog, error)
	UpdateFetchLog(log *models.FetchLog) error
	CleanupOldArticles(daysToKeep int) (int, error)
	GetArticlesNeedingOGImage(limit int) ([]database.ArticleForBackfill, error)
	UpdateArticleImage(urlHash string, imageURL string) error
	GetArticlesNeedingContent(limit int) ([]database.ArticleForContentBackfill, error)
	UpdateArticleContent(urlHash string, content string) error
}

// backfillConfig holds parameters for a generic backfill operation.
type backfillConfig[T any] struct {
	name       string
	timeout    time.Duration
	limit      int
	maxWorkers int
	fetch      func(limit int) ([]T, error)
	process    func(ctx context.Context, item T) bool
}

// runBackfill executes a generic backfill operation: fetch items, then process
// them concurrently with a worker pool.
func runBackfill[T any](cfg backfillConfig[T]) {
	log.Printf("Starting %s backfill", cfg.name)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	items, err := cfg.fetch(cfg.limit)
	if err != nil {
		log.Fatalf("Failed to get items for %s backfill: %v", cfg.name, err)
	}

	log.Printf("Found %d items needing %s backfill", len(items), cfg.name)

	if len(items) == 0 {
		log.Printf("No items need %s backfill", cfg.name)
		return
	}

	numWorkers := min(cfg.maxWorkers, len(items))
	work := make(chan T, len(items))
	results := make(chan struct{ updated bool }, len(items))
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range work {
				select {
				case <-ctx.Done():
					results <- struct{ updated bool }{false}
					return
				default:
					results <- struct{ updated bool }{cfg.process(ctx, item)}
				}
			}
		}()
	}

	for _, item := range items {
		work <- item
	}
	close(work)

	go func() {
		wg.Wait()
		close(results)
	}()

	var updatedCount, skippedCount int
	for result := range results {
		if result.updated {
			updatedCount++
		} else {
			skippedCount++
		}
	}

	log.Printf("%s backfill complete: updated=%d, skipped=%d", cfg.name, updatedCount, skippedCount)
}

func main() {
	log.Println("🚀 Starting Pulse RSS Worker")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize components
	var db Store = database.NewClient(cfg)
	rssParser := parser.New()

	// Check for special commands
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "cleanup":
			runCleanup(db, cfg.ArticleRetentionDays)
			return
		case "backfill-images":
			runOGImageBackfill(db)
			return
		case "backfill-content":
			runContentBackfill(db)
			return
		}
	}

	// Run the main fetch process
	if err := runFetch(db, rssParser, cfg.MaxConcurrent); err != nil {
		log.Fatalf("Fetch failed: %v", err)
	}

	log.Println("✅ RSS Worker completed successfully")
}

// runFetch executes the main RSS feed fetching process. It retrieves all active
// sources from the database, processes them concurrently (limited by maxConcurrent),
// and inserts new articles. Progress and results are logged to the fetch_logs table.
//
// Individual source failures are logged but do not cause the function to return
// an error. The function only returns an error for critical failures such as
// being unable to retrieve the source list.
func runFetch(db Store, rssParser *parser.Parser, maxConcurrent int) error {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()

	// Create fetch log
	fetchLog, err := db.CreateFetchLog()
	if err != nil {
		log.Printf("Warning: Failed to create fetch log: %v", err)
		// Continue anyway, logging is not critical
		fetchLog = &models.FetchLog{
			StartedAt: time.Now(),
			Status:    "running",
			Errors:    []string{},
		}
	}

	// Get active sources
	sources, err := db.GetActiveSources()
	if err != nil {
		fetchLog.Status = "failed"
		fetchLog.Errors = append(fetchLog.Errors, fmt.Sprintf("Failed to get sources: %v", err))
		if logErr := db.UpdateFetchLog(fetchLog); logErr != nil {
			log.Printf("Warning: Failed to update fetch log: %v", logErr)
		}
		return fmt.Errorf("failed to get sources: %w", err)
	}

	log.Printf("📡 Found %d active sources", len(sources))

	// Process sources concurrently with a semaphore
	results := make(chan models.FetchResult, len(sources))
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, maxConcurrent)

	for _, source := range sources {
		wg.Add(1)
		go func(s models.Source) {
			defer wg.Done()
			semaphore <- struct{}{}        // Acquire
			defer func() { <-semaphore }() // Release

			result := processSource(ctx, db, rssParser, s)
			results <- result
		}(source)
	}

	// Close results channel when all goroutines complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	var totalFetched, totalInserted, totalSkipped int
	var errors []string

	for result := range results {
		fetchLog.SourcesProcessed++
		totalFetched += result.ArticlesFetched
		totalInserted += result.ArticlesInserted
		totalSkipped += result.ArticlesSkipped

		if result.Error != nil {
			errMsg := fmt.Sprintf("%s: %v", result.Source.Name, result.Error)
			errors = append(errors, errMsg)
			log.Printf("❌ %s", errMsg)
		} else {
			log.Printf("✓ %s: fetched=%d, inserted=%d, skipped=%d",
				result.Source.Name, result.ArticlesFetched, result.ArticlesInserted, result.ArticlesSkipped)
		}
	}

	// Update fetch log
	fetchLog.ArticlesFetched = totalFetched
	fetchLog.ArticlesInserted = totalInserted
	fetchLog.ArticlesSkipped = totalSkipped
	fetchLog.Errors = errors
	fetchLog.Status = "completed"
	if len(errors) > 0 && fetchLog.SourcesProcessed == len(errors) {
		fetchLog.Status = "failed"
	}

	if fetchLog.ID != "" {
		if logErr := db.UpdateFetchLog(fetchLog); logErr != nil {
			log.Printf("Warning: Failed to update fetch log: %v", logErr)
		}
	}

	// Print summary
	log.Printf("📊 Summary: sources=%d, fetched=%d, inserted=%d, skipped=%d, errors=%d",
		fetchLog.SourcesProcessed, totalFetched, totalInserted, totalSkipped, len(errors))

	return nil
}

// processSource fetches and processes a single RSS source. It parses the feed,
// inserts new articles into the database (deduplicating by URL hash), and updates
// the source's last_fetched_at timestamp. Returns a FetchResult containing
// counts of articles fetched, inserted, and skipped, plus any error encountered.
func processSource(ctx context.Context, db Store, rssParser *parser.Parser, source models.Source) models.FetchResult {
	result := models.FetchResult{Source: source}

	// Parse the RSS feed
	articles, err := rssParser.ParseFeed(ctx, source)
	if err != nil {
		result.Error = fmt.Errorf("parse error: %w", err)
		return result
	}

	result.ArticlesFetched = len(articles)

	// Insert articles
	inserted, skipped, err := db.InsertArticles(articles)
	if err != nil {
		result.Error = fmt.Errorf("insert error: %w", err)
		return result
	}

	result.ArticlesInserted = inserted
	result.ArticlesSkipped = skipped

	// Update source last_fetched_at
	if err := db.UpdateSourceLastFetched(source.ID); err != nil {
		log.Printf("Warning: Failed to update last_fetched_at for %s: %v", source.Name, err)
	}

	return result
}

// runCleanup removes articles older than the specified retention period.
// It calls the database's cleanup_old_articles function and logs the count
// of deleted articles. Exits fatally if the cleanup operation fails.
func runCleanup(db Store, daysToKeep int) {
	log.Printf("🧹 Running cleanup (keeping %d days of articles)", daysToKeep)

	deleted, err := db.CleanupOldArticles(daysToKeep)
	if err != nil {
		log.Fatalf("Cleanup failed: %v", err)
	}

	log.Printf("✅ Cleanup complete: deleted %d old articles", deleted)
}

// runOGImageBackfill fetches og:image URLs for articles that are missing
// high-resolution images using the generic backfill runner.
func runOGImageBackfill(db Store) {
	ogExtractor := parser.NewOGImageExtractor()
	runBackfill(backfillConfig[database.ArticleForBackfill]{
		name:       "og:image",
		timeout:    ogImageBackfillTimeout,
		limit:      ogImageBackfillLimit,
		maxWorkers: ogImageBackfillWorkers,
		fetch:      db.GetArticlesNeedingOGImage,
		process: func(ctx context.Context, article database.ArticleForBackfill) bool {
			return processOGImageBackfill(ctx, db, ogExtractor, article)
		},
	})
}

// processOGImageBackfill attempts to extract the og:image URL from a single
// article's webpage. If a valid image is found and differs from the current
// image, updates the database. Returns true if the article was updated.
func processOGImageBackfill(ctx context.Context, db Store, ogExtractor *parser.OGImageExtractor, article database.ArticleForBackfill) bool {
	ogImage, err := ogExtractor.ExtractOGImage(ctx, article.URL)
	if err != nil {
		log.Printf("[BACKFILL] ERROR fetching og:image for %s: %v", article.URL, err)
		return false
	}

	if ogImage == "" {
		log.Printf("[BACKFILL] No og:image found for %s", article.URL)
		return false
	}

	// Only update if og:image is different from current image
	if article.ImageURL != nil && ogImage == *article.ImageURL {
		log.Printf("[BACKFILL] Same image for %s", article.URL)
		return false
	}

	// Update the article's image_url
	if err := db.UpdateArticleImage(article.URLHash, ogImage); err != nil {
		log.Printf("[BACKFILL] ERROR updating %s: %v", article.URL, err)
		return false
	}

	log.Printf("[BACKFILL] SUCCESS %s -> %s", article.URL, ogImage)
	return true
}

// runContentBackfill extracts full article content for articles that are
// missing the content field using the generic backfill runner.
func runContentBackfill(db Store) {
	contentExtractor := parser.NewContentExtractor()
	runBackfill(backfillConfig[database.ArticleForContentBackfill]{
		name:       "content",
		timeout:    contentBackfillTimeout,
		limit:      contentBackfillLimit,
		maxWorkers: contentBackfillWorkers,
		fetch:      db.GetArticlesNeedingContent,
		process: func(ctx context.Context, article database.ArticleForContentBackfill) bool {
			return processContentBackfill(ctx, db, contentExtractor, article)
		},
	})
}

// processContentBackfill uses go-readability to extract the main text content
// from a single article's webpage. If valid content is extracted, updates
// the database. Returns true if the article was updated.
func processContentBackfill(ctx context.Context, db Store, contentExtractor *parser.ContentExtractor, article database.ArticleForContentBackfill) bool {
	content, err := contentExtractor.ExtractTextContent(ctx, article.URL)
	if err != nil {
		log.Printf("[CONTENT-BACKFILL] ERROR fetching content for %s: %v", article.URL, err)
		return false
	}

	if content == "" {
		log.Printf("[CONTENT-BACKFILL] No content extracted for %s", article.URL)
		return false
	}

	// Update the article's content
	if err := db.UpdateArticleContent(article.URLHash, content); err != nil {
		log.Printf("[CONTENT-BACKFILL] ERROR updating %s: %v", article.URL, err)
		return false
	}

	log.Printf("[CONTENT-BACKFILL] SUCCESS %s (%d chars)", article.URL, len(content))
	return true
}
