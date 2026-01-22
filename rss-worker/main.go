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

func main() {
	log.Println("🚀 Starting Pulse RSS Worker")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize components
	db := database.NewClient(cfg)
	rssParser := parser.New()

	// Check for special commands
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "cleanup":
			runCleanup(db, cfg.ArticleRetentionDays)
			return
		case "backfill-images":
			runOGImageBackfill(db, rssParser)
			return
		}
	}

	// Run the main fetch process
	if err := runFetch(db, rssParser, cfg.MaxConcurrent); err != nil {
		log.Fatalf("Fetch failed: %v", err)
	}

	log.Println("✅ RSS Worker completed successfully")
}

func runFetch(db *database.Client, rssParser *parser.Parser, maxConcurrent int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
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
		db.UpdateFetchLog(fetchLog)
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
		db.UpdateFetchLog(fetchLog)
	}

	// Print summary
	log.Printf("📊 Summary: sources=%d, fetched=%d, inserted=%d, skipped=%d, errors=%d",
		fetchLog.SourcesProcessed, totalFetched, totalInserted, totalSkipped, len(errors))

	return nil
}

func processSource(ctx context.Context, db *database.Client, rssParser *parser.Parser, source models.Source) models.FetchResult {
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

func runCleanup(db *database.Client, daysToKeep int) {
	log.Printf("🧹 Running cleanup (keeping %d days of articles)", daysToKeep)

	deleted, err := db.CleanupOldArticles(daysToKeep)
	if err != nil {
		log.Fatalf("Cleanup failed: %v", err)
	}

	log.Printf("✅ Cleanup complete: deleted %d old articles", deleted)
}

func runOGImageBackfill(db *database.Client, rssParser *parser.Parser) {
	log.Println("🖼️ Starting og:image backfill for articles missing high-res images")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// Get articles that need og:image backfill (limit to 500 per run)
	articles, err := db.GetArticlesNeedingOGImage(500)
	if err != nil {
		log.Fatalf("Failed to get articles for backfill: %v", err)
	}

	log.Printf("📋 Found %d articles needing og:image backfill", len(articles))

	if len(articles) == 0 {
		log.Println("✅ No articles need og:image backfill")
		return
	}

	// Process articles concurrently
	const maxWorkers = 5
	numWorkers := min(maxWorkers, len(articles))
	work := make(chan database.ArticleForBackfill, len(articles))
	results := make(chan struct{ updated bool }, len(articles))
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for article := range work {
				select {
				case <-ctx.Done():
					results <- struct{ updated bool }{false}
					return
				default:
					updated := processOGImageBackfill(ctx, db, rssParser, article)
					results <- struct{ updated bool }{updated}
				}
			}
		}()
	}

	// Send work
	for _, article := range articles {
		work <- article
	}
	close(work)

	// Collect results
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

	log.Printf("✅ Backfill complete: updated=%d, skipped=%d", updatedCount, skippedCount)
}

func processOGImageBackfill(ctx context.Context, db *database.Client, rssParser *parser.Parser, article database.ArticleForBackfill) bool {
	ogExtractor := parser.NewOGImageExtractor()
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
