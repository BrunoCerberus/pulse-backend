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
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/pulsefeed/rss-worker/internal/config"
	"github.com/pulsefeed/rss-worker/internal/database"
	"github.com/pulsefeed/rss-worker/internal/logger"
	"github.com/pulsefeed/rss-worker/internal/models"
	"github.com/pulsefeed/rss-worker/internal/parser"
)

// newRunID returns a short hex ID (8 chars) for correlating all log lines
// emitted by a single worker invocation. Falls back to a timestamp if
// crypto/rand fails.
func newRunID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("ts-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

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

// Backfill attempt-tracking defaults. Override via main() using values from
// config.Load() so tests can exercise run*Backfill without requiring SUPABASE
// environment variables.
var (
	backfillMaxAttempts   = 3
	backfillCooldownHours = 24
)

// Circuit breaker defaults; main() overrides from config at startup so runFetch
// and tests can share the same knobs without reloading config.
var (
	circuitFailureThreshold = 5
	circuitBaseBackoffHours = 1
	circuitMaxBackoffHours  = 24
)

// Store abstracts the database operations used by the worker commands.
// This allows testing with mock implementations and decouples main from
// the concrete database.Client type.
type Store interface {
	GetActiveSources() ([]models.Source, error)
	InsertArticles(articles []*models.Article) (inserted int, skipped int, err error)
	BatchUpdateSourceFetchState(updates []database.SourceFetchState) error
	CreateFetchLog() (*models.FetchLog, error)
	UpdateFetchLog(log *models.FetchLog) error
	CleanupOldArticles(daysToKeep int) (int, error)
	CleanupOldFetchLogs(daysToKeep int) (int, error)
	GetArticlesNeedingOGImage(limit, maxAttempts, cooldownHours int) ([]database.ArticleForBackfill, error)
	UpdateArticleImage(urlHash string, imageURL string) error
	GetArticlesNeedingContent(limit, maxAttempts, cooldownHours int) ([]database.ArticleForContentBackfill, error)
	UpdateArticleContent(urlHash string, content string) error
	BumpBackfillAttempts(urlHashes []string, kind string) error
}

// nextCircuitOpenUntil returns the time the circuit should stay open after
// `failures` consecutive failures, or nil when the circuit should remain closed
// (failures below threshold, or the caller passed a non-positive base/threshold).
//
// Math: for failures >= threshold, delay = base * 2^(failures - threshold),
// capped at maxHours. At threshold → base; +1 → 2*base; +2 → 4*base; …
func nextCircuitOpenUntil(now time.Time, failures, threshold, baseHours, maxHours int) *time.Time {
	if failures < threshold || threshold <= 0 || baseHours <= 0 {
		return nil
	}
	exp := failures - threshold
	// Cap the shift so `baseHours << exp` can't overflow int.
	const maxShift = 30
	if exp > maxShift {
		exp = maxShift
	}
	delay := baseHours << uint(exp)
	if maxHours > 0 && delay > maxHours {
		delay = maxHours
	}
	t := now.Add(time.Duration(delay) * time.Hour)
	return &t
}

// buildSourceFetchState derives the next persisted state for a source from its
// previous state and the outcome of the current fetch. Pure function — callers
// pass `now` so tests can assert exact timestamps.
func buildSourceFetchState(source models.Source, result models.FetchResult, now time.Time) database.SourceFetchState {
	state := database.SourceFetchState{ID: source.ID}

	if result.Error == nil {
		// Success (including 304): reset circuit, record fresh validators,
		// stamp last_fetched_at so the adaptive interval advances.
		state.ConsecutiveFailures = 0
		state.CircuitOpenUntil = nil
		if result.ETag != "" {
			v := result.ETag
			state.ETag = &v
		}
		if result.LastModified != "" {
			v := result.LastModified
			state.LastModified = &v
		}
		state.LastFetchedAt = &now
		return state
	}

	// Failure: increment counter, maybe trip the circuit. Preserve existing
	// validators so a transient error doesn't force a full refetch on recovery.
	// Don't stamp last_fetched_at — keeping the old value lets the adaptive
	// interval still retry on the normal cadence.
	state.ConsecutiveFailures = source.ConsecutiveFailures + 1
	state.CircuitOpenUntil = nextCircuitOpenUntil(
		now,
		state.ConsecutiveFailures,
		circuitFailureThreshold,
		circuitBaseBackoffHours,
		circuitMaxBackoffHours,
	)
	state.ETag = source.ETag
	state.LastModified = source.LastModified
	return state
}

// backfillConfig holds parameters for a generic backfill operation.
type backfillConfig[T any] struct {
	name          string
	kind          string // "image" or "content" — passed to BumpBackfillAttempts
	timeout       time.Duration
	limit         int
	maxAttempts   int
	cooldownHours int
	maxWorkers    int
	fetch         func(limit, maxAttempts, cooldownHours int) ([]T, error)
	process       func(ctx context.Context, item T) bool
	hashOf        func(item T) string
	bumpAttempts  func(hashes []string, kind string) error
}

// backfillOutcome carries the per-item result out of the worker pool so the
// caller can tell which articles were attempted-but-failed and persist an
// attempt bump for them.
type backfillOutcome struct {
	updated bool
	hash    string // url_hash of the attempted item; "" when skipped by cancel
}

// runBackfill executes a generic backfill operation: fetch items, then process
// them concurrently with a worker pool. Returns an error if fetching items fails.
// The baseCtx is derived from the top-level signal-aware context so SIGTERM
// propagates into worker goroutines and in-flight HTTP requests.
func runBackfill[T any](baseCtx context.Context, cfg backfillConfig[T]) error {
	logger.Infof("Starting %s backfill", cfg.name)

	ctx, cancel := context.WithTimeout(baseCtx, cfg.timeout)
	defer cancel()

	items, err := cfg.fetch(cfg.limit, cfg.maxAttempts, cfg.cooldownHours)
	if err != nil {
		return fmt.Errorf("failed to get items for %s backfill: %w", cfg.name, err)
	}

	logger.Infof("Found %d items needing %s backfill", len(items), cfg.name)

	if len(items) == 0 {
		logger.Infof("No items need %s backfill", cfg.name)
		return nil
	}

	numWorkers := min(cfg.maxWorkers, len(items))
	work := make(chan T, numWorkers*2)
	results := make(chan backfillOutcome, numWorkers*2)
	var wg sync.WaitGroup

	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range work {
				select {
				case <-ctx.Done():
					// Skipped due to cancellation: empty hash so we don't
					// penalize the article for a shutdown we caused.
					results <- backfillOutcome{updated: false, hash: ""}
					return
				default:
					results <- backfillOutcome{
						updated: cfg.process(ctx, item),
						hash:    cfg.hashOf(item),
					}
				}
			}
		}()
	}

	for _, item := range items {
		select {
		case work <- item:
		case <-ctx.Done():
		}
	}
	close(work)

	go func() {
		wg.Wait()
		close(results)
	}()

	var updatedCount, skippedCount int
	var failedHashes []string
	for r := range results {
		if r.updated {
			updatedCount++
		} else {
			skippedCount++
			if r.hash != "" {
				failedHashes = append(failedHashes, r.hash)
			}
		}
	}

	// Persist one attempt per failed article so the next run honors the
	// cooldown and eventually gives up once max_attempts is reached.
	if len(failedHashes) > 0 && cfg.bumpAttempts != nil {
		if err := cfg.bumpAttempts(failedHashes, cfg.kind); err != nil {
			logger.Warnf("Failed to bump %s backfill attempts: %v", cfg.name, err)
		}
	}

	logger.Infof("%s backfill complete: updated=%d, skipped=%d", cfg.name, updatedCount, skippedCount)
	return nil
}

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		logger.Fatalf("Failed to load config: %v", err)
	}

	// Apply env-driven per-host rate limit before constructing any HTTP
	// clients so every parser/extractor picks up the override.
	parser.SetHostRateLimit(cfg.HostRateLimitRPS, cfg.HostRateLimitBurst)

	// Propagate backfill attempt-tracking knobs to package vars so run*Backfill
	// picks them up without reloading config.
	backfillMaxAttempts = cfg.BackfillMaxAttempts
	backfillCooldownHours = cfg.BackfillCooldownHours

	// Propagate circuit breaker knobs so buildSourceFetchState uses the
	// env-driven values without re-reading config per source.
	circuitFailureThreshold = cfg.CircuitFailureThreshold
	circuitBaseBackoffHours = cfg.CircuitBaseBackoffHours
	circuitMaxBackoffHours = cfg.CircuitMaxBackoffHours

	// Initialize components
	var db Store = database.NewClient(cfg)
	rssParser := parser.New()

	// Signal-aware root context: SIGINT/SIGTERM cancels in-flight work so
	// GitHub Actions cancellations or runner rotations exit cleanly.
	baseCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Determine command and tag every log line in this run with the same ID
	// so operators can correlate events across goroutines and structured
	// exports (LOG_FORMAT=json).
	command := "fetch"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	runID := newRunID()
	logger.With("run_id", runID, "command", command).Info("run_started")

	// Check for special commands
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "cleanup":
			runCleanup(baseCtx, db, cfg.ArticleRetentionDays)
			return
		case "backfill-images":
			if err := runOGImageBackfill(baseCtx, db); err != nil && !errors.Is(err, context.Canceled) {
				logger.Fatalf("Image backfill failed: %v", err)
			}
			return
		case "backfill-content":
			if err := runContentBackfill(baseCtx, db); err != nil && !errors.Is(err, context.Canceled) {
				logger.Fatalf("Content backfill failed: %v", err)
			}
			return
		}
	}

	// Run the main fetch process
	if err := runFetch(baseCtx, db, rssParser, cfg.MaxConcurrent); err != nil && !errors.Is(err, context.Canceled) {
		logger.Fatalf("Fetch failed: %v", err)
	}

	logger.With("run_id", runID, "command", command).Info("run_completed")
}

// runFetch executes the main RSS feed fetching process. It retrieves all active
// sources from the database, processes them concurrently (limited by maxConcurrent),
// and inserts new articles. Progress and results are logged to the fetch_logs table.
//
// Individual source failures are logged but do not cause the function to return
// an error. The function only returns an error for critical failures such as
// being unable to retrieve the source list.
func runFetch(baseCtx context.Context, db Store, rssParser *parser.Parser, maxConcurrent int) error {
	ctx, cancel := context.WithTimeout(baseCtx, fetchTimeout)
	defer cancel()

	// Create fetch log
	fetchLog, err := db.CreateFetchLog()
	if err != nil {
		logger.Warnf("Failed to create fetch log: %v", err)
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
			logger.Warnf("Failed to update fetch log: %v", logErr)
		}
		return fmt.Errorf("failed to get sources: %w", err)
	}

	logger.Infof("📡 Found %d active sources", len(sources))

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
			defer func() {
				if r := recover(); r != nil {
					results <- models.FetchResult{
						Source: s,
						Error:  fmt.Errorf("panic in processSource: %v", r),
					}
				}
			}()

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
	var fetchStateUpdates []database.SourceFetchState
	now := time.Now().UTC()

	for result := range results {
		fetchLog.SourcesProcessed++
		totalFetched += result.ArticlesFetched
		totalInserted += result.ArticlesInserted
		totalSkipped += result.ArticlesSkipped

		sourceLog := logger.With(
			"source_id", result.Source.ID,
			"source_name", result.Source.Name,
			"fetched", result.ArticlesFetched,
			"inserted", result.ArticlesInserted,
			"skipped", result.ArticlesSkipped,
			"not_modified", result.NotModified,
		)
		if result.Error != nil {
			errMsg := fmt.Sprintf("%s: %v", result.Source.Name, result.Error)
			errors = append(errors, errMsg)
			sourceLog.Error("source_failed",
				"err", result.Error.Error(),
				"consecutive_failures", result.Source.ConsecutiveFailures+1,
			)
		} else if result.NotModified {
			sourceLog.Info("source_not_modified")
		} else {
			sourceLog.Info("source_succeeded")
		}

		// Persist per-source state for every source (success or failure) so
		// the circuit breaker counter advances/resets and cache validators
		// are captured or preserved.
		fetchStateUpdates = append(fetchStateUpdates, buildSourceFetchState(result.Source, result, now))
	}

	// One round-trip to record all per-source state changes.
	if len(fetchStateUpdates) > 0 {
		if err := db.BatchUpdateSourceFetchState(fetchStateUpdates); err != nil {
			logger.Warnf("Failed to batch update source fetch state: %v", err)
		}
	}

	// Update fetch log
	fetchLog.ArticlesFetched = totalFetched
	fetchLog.ArticlesInserted = totalInserted
	fetchLog.ArticlesSkipped = totalSkipped
	fetchLog.Errors = errors
	fetchLog.Status = "completed"
	if len(errors) > 0 {
		if fetchLog.SourcesProcessed == len(errors) {
			fetchLog.Status = "failed"
		} else {
			fetchLog.Status = "partial_failure"
		}
	}

	if fetchLog.ID != "" {
		if logErr := db.UpdateFetchLog(fetchLog); logErr != nil {
			logger.Warnf("Failed to update fetch log: %v", logErr)
		}
	}

	// Print summary
	logger.Infof("📊 Summary: sources=%d, fetched=%d, inserted=%d, skipped=%d, errors=%d",
		fetchLog.SourcesProcessed, totalFetched, totalInserted, totalSkipped, len(errors))

	return nil
}

// processSource fetches and processes a single RSS source. Parses the feed
// with conditional-GET validators, inserts new articles, and passes back the
// response ETag/Last-Modified + NotModified flag so runFetch can persist the
// per-source state (circuit breaker + cache validators) in a batch update.
//
// A 304 Not Modified response is a success (NotModified=true, zero articles,
// nil Error) — the caller resets the circuit and re-records the validators.
func processSource(ctx context.Context, db Store, rssParser *parser.Parser, source models.Source) models.FetchResult {
	result := models.FetchResult{Source: source}

	parseResult, err := rssParser.ParseFeed(ctx, source)
	if err != nil {
		result.Error = fmt.Errorf("parse error: %w", err)
		return result
	}

	result.ETag = parseResult.ETag
	result.LastModified = parseResult.LastModified
	result.NotModified = parseResult.NotModified

	if parseResult.NotModified {
		// Nothing new to insert; still a successful fetch.
		return result
	}

	result.ArticlesFetched = len(parseResult.Articles)

	// Insert articles (may return partial results alongside an error)
	inserted, skipped, err := db.InsertArticles(parseResult.Articles)
	result.ArticlesInserted = inserted
	result.ArticlesSkipped = skipped
	if err != nil {
		result.Error = fmt.Errorf("insert error (partial): %w", err)
	}

	return result
}

// runCleanup removes articles older than the specified retention period.
// It calls the database's cleanup_old_articles function and logs the count
// of deleted articles. Exits fatally if the cleanup operation fails.
// If ctx is already cancelled (signal received before we start), skip cleanly.
func runCleanup(ctx context.Context, db Store, daysToKeep int) {
	if err := ctx.Err(); err != nil {
		logger.Infof("Cleanup skipped: %v", err)
		return
	}

	logger.Infof("🧹 Running cleanup (keeping %d days of articles)", daysToKeep)

	deleted, err := db.CleanupOldArticles(daysToKeep)
	if err != nil {
		logger.Fatalf("Cleanup failed: %v", err)
	}

	logger.Infof("✅ Cleanup complete: deleted %d old articles", deleted)

	// Clean up old fetch logs (non-fatal on error)
	logsDeleted, err := db.CleanupOldFetchLogs(daysToKeep)
	if err != nil {
		logger.Warnf("Failed to cleanup old fetch logs: %v", err)
	} else {
		logger.Infof("🧹 Cleaned up %d old fetch logs", logsDeleted)
	}
}

// runOGImageBackfill fetches og:image URLs for articles that are missing
// high-resolution images using the generic backfill runner.
func runOGImageBackfill(ctx context.Context, db Store) error {
	ogExtractor := parser.NewOGImageExtractor()
	return runBackfill(ctx, backfillConfig[database.ArticleForBackfill]{
		name:          "og:image",
		kind:          "image",
		timeout:       ogImageBackfillTimeout,
		limit:         ogImageBackfillLimit,
		maxAttempts:   backfillMaxAttempts,
		cooldownHours: backfillCooldownHours,
		maxWorkers:    ogImageBackfillWorkers,
		fetch:         db.GetArticlesNeedingOGImage,
		process: func(ctx context.Context, article database.ArticleForBackfill) bool {
			return processOGImageBackfill(ctx, db, ogExtractor, article)
		},
		hashOf:       func(a database.ArticleForBackfill) string { return a.URLHash },
		bumpAttempts: db.BumpBackfillAttempts,
	})
}

// processOGImageBackfill attempts to extract the og:image URL from a single
// article's webpage. If a valid image is found and differs from the current
// image, updates the database. Returns true if the article was updated.
func processOGImageBackfill(ctx context.Context, db Store, ogExtractor *parser.OGImageExtractor, article database.ArticleForBackfill) bool {
	articleLog := logger.With("kind", "og_image", "url_hash", article.URLHash, "url", article.URL)

	ogImage, err := ogExtractor.ExtractOGImage(ctx, article.URL)
	if err != nil {
		articleLog.Info("backfill_fetch_error", "err", err.Error())
		return false
	}

	if ogImage == "" {
		articleLog.Debug("backfill_not_found")
		return false
	}

	// Only update if og:image is different from current image
	if article.ImageURL != nil && ogImage == *article.ImageURL {
		articleLog.Debug("backfill_same_image")
		return false
	}

	// Update the article's image_url
	if err := db.UpdateArticleImage(article.URLHash, ogImage); err != nil {
		articleLog.Error("backfill_update_error", "err", err.Error())
		return false
	}

	articleLog.Info("backfill_success", "og_image", ogImage)
	return true
}

// runContentBackfill extracts full article content for articles that are
// missing the content field using the generic backfill runner.
func runContentBackfill(ctx context.Context, db Store) error {
	contentExtractor := parser.NewContentExtractor()
	return runBackfill(ctx, backfillConfig[database.ArticleForContentBackfill]{
		name:          "content",
		kind:          "content",
		timeout:       contentBackfillTimeout,
		limit:         contentBackfillLimit,
		maxAttempts:   backfillMaxAttempts,
		cooldownHours: backfillCooldownHours,
		maxWorkers:    contentBackfillWorkers,
		fetch:         db.GetArticlesNeedingContent,
		process: func(ctx context.Context, article database.ArticleForContentBackfill) bool {
			return processContentBackfill(ctx, db, contentExtractor, article)
		},
		hashOf:       func(a database.ArticleForContentBackfill) string { return a.URLHash },
		bumpAttempts: db.BumpBackfillAttempts,
	})
}

// processContentBackfill uses go-readability to extract the main text content
// from a single article's webpage. If valid content is extracted, updates
// the database. Returns true if the article was updated.
func processContentBackfill(ctx context.Context, db Store, contentExtractor *parser.ContentExtractor, article database.ArticleForContentBackfill) bool {
	articleLog := logger.With("kind", "content", "url_hash", article.URLHash, "url", article.URL)

	content, err := contentExtractor.ExtractTextContent(ctx, article.URL)
	if err != nil {
		articleLog.Info("backfill_fetch_error", "err", err.Error())
		return false
	}

	if content == "" {
		articleLog.Debug("backfill_not_found")
		return false
	}

	// Update the article's content
	if err := db.UpdateArticleContent(article.URLHash, content); err != nil {
		articleLog.Error("backfill_update_error", "err", err.Error())
		return false
	}

	articleLog.Info("backfill_success", "chars", len(content))
	return true
}
