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

// randRead is crypto/rand.Read indirected through a package variable so the
// failure-fallback branch of newRunID is testable.
var randRead = rand.Read

// newRunID returns a short hex ID (8 chars) for correlating all log lines
// emitted by a single worker invocation. Falls back to a timestamp if
// crypto/rand fails.
func newRunID() string {
	var b [4]byte
	if _, err := randRead(b[:]); err != nil {
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
	BatchUpdateArticleImages(updates []database.ImageUpdate) error
	GetArticlesNeedingContent(limit, maxAttempts, cooldownHours int) ([]database.ArticleForContentBackfill, error)
	BatchUpdateArticleContent(updates []database.ContentUpdate) error
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
	// (`exp` is already non-negative because of the `failures < threshold` guard above.)
	const maxShift = 30
	if exp > maxShift {
		exp = maxShift
	}
	delay := baseHours << exp
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

	// Producer runs in its own goroutine so the results consumer (the main
	// loop below) can drain results concurrently with item enqueueing. If
	// they shared a goroutine, the consumer wouldn't start until the
	// producer finished — and the producer can't finish while workers
	// are blocked pushing to a full results channel. That deadlock was the
	// reason the original implementation stalled after exactly 3 worker
	// cycles per goroutine (work_buffer + results_buffer + one stuck push).
	go func() {
		for _, item := range items {
			select {
			case work <- item:
			case <-ctx.Done():
			}
		}
		close(work)
	}()

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

	// Defence-in-depth: scrub the service-role key from the environment so a
	// future bug that exec's a subprocess (or anything that dumps env to
	// logs) can't leak it. cfg.SupabaseKey already holds the live value.
	// On Linux/macOS, Unsetenv with a non-empty name cannot return an error.
	_ = os.Unsetenv("SUPABASE_SERVICE_ROLE_KEY")

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

// imageFlushBatchSize is the chunk size used when flushing queued image
// updates via the batch RPC. 50 mirrors the article-insert batch and keeps
// the JSON payload comfortably under PostgREST/Supabase limits.
const imageFlushBatchSize = 50

// runOGImageBackfill fetches og:image URLs for articles missing high-res
// images and writes them via a single batched RPC at the end of the run.
//
// Per-article extractions are concurrent via the generic backfill runner;
// each successful extraction enqueues an ImageUpdate to an in-memory slice
// rather than issuing a per-row PATCH. The slice is flushed in chunks of
// imageFlushBatchSize after the worker pool drains — turning ~500
// single-row UPDATEs per run into ~10 RPC calls.
func runOGImageBackfill(ctx context.Context, db Store) error {
	ogExtractor := parser.NewOGImageExtractor()

	var (
		mu      sync.Mutex
		pending []database.ImageUpdate
	)
	queue := func(update database.ImageUpdate) {
		mu.Lock()
		pending = append(pending, update)
		mu.Unlock()
	}

	err := runBackfill(ctx, backfillConfig[database.ArticleForBackfill]{
		name:          "og:image",
		kind:          "image",
		timeout:       ogImageBackfillTimeout,
		limit:         ogImageBackfillLimit,
		maxAttempts:   backfillMaxAttempts,
		cooldownHours: backfillCooldownHours,
		maxWorkers:    ogImageBackfillWorkers,
		fetch:         db.GetArticlesNeedingOGImage,
		process: func(ctx context.Context, article database.ArticleForBackfill) bool {
			return processOGImageBackfill(ctx, ogExtractor, article, queue)
		},
		hashOf:       func(a database.ArticleForBackfill) string { return a.URLHash },
		bumpAttempts: db.BumpBackfillAttempts,
	})

	// Flush whatever was queued even if runBackfill errored or was canceled —
	// partial progress is more valuable than none. Articles whose writes fail
	// here keep `image_url IS NULL` and resurface in the next run's candidate
	// set, so there's no risk of permanent loss.
	if flushErr := flushImageUpdates(db, pending); flushErr != nil {
		logger.Warnf("Failed to flush og:image updates (%d queued): %v", len(pending), flushErr)
	} else if len(pending) > 0 {
		logger.Infof("Flushed %d og:image updates", len(pending))
	}

	return err
}

// processOGImageBackfill attempts to extract the og:image URL from a single
// article's webpage. If a valid image is found and differs from the current
// image, enqueues an ImageUpdate for the caller to batch-write later.
// Returns true if an update was queued.
func processOGImageBackfill(ctx context.Context, ogExtractor *parser.OGImageExtractor, article database.ArticleForBackfill, queue func(database.ImageUpdate)) bool {
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

	queue(database.ImageUpdate{
		URLHash:  article.URLHash,
		ImageURL: ogImage,
	})
	articleLog.Info("backfill_queued", "og_image", ogImage)
	return true
}

// flushImageUpdates writes the queued image updates via BatchUpdateArticleImages
// in chunks of imageFlushBatchSize. Per-batch errors are accumulated and
// returned together so one bad batch doesn't drop the rest.
func flushImageUpdates(db Store, updates []database.ImageUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	var errs []error
	for i := 0; i < len(updates); i += imageFlushBatchSize {
		end := i + imageFlushBatchSize
		if end > len(updates) {
			end = len(updates)
		}
		if err := db.BatchUpdateArticleImages(updates[i:end]); err != nil {
			errs = append(errs, fmt.Errorf("batch %d-%d: %w", i, end, err))
		}
	}
	return errors.Join(errs...)
}

// contentFlushBatchSize chunks the post-pool content flush. 50 mirrors the
// image flush; content payloads are larger (text bodies), but jsonb_to_recordset
// handles them comfortably and 50 keeps the request body well within limits.
const contentFlushBatchSize = 50

// runContentBackfill extracts full article content for articles that are
// missing the content field and writes the results via the batch RPC at
// the end of the run. Same queue/flush pattern as runOGImageBackfill —
// turns per-row PATCHes into one RPC per chunk.
func runContentBackfill(ctx context.Context, db Store) error {
	contentExtractor := parser.NewContentExtractor()

	var (
		mu      sync.Mutex
		pending []database.ContentUpdate
	)
	queue := func(update database.ContentUpdate) {
		mu.Lock()
		pending = append(pending, update)
		mu.Unlock()
	}

	err := runBackfill(ctx, backfillConfig[database.ArticleForContentBackfill]{
		name:          "content",
		kind:          "content",
		timeout:       contentBackfillTimeout,
		limit:         contentBackfillLimit,
		maxAttempts:   backfillMaxAttempts,
		cooldownHours: backfillCooldownHours,
		maxWorkers:    contentBackfillWorkers,
		fetch:         db.GetArticlesNeedingContent,
		process: func(ctx context.Context, article database.ArticleForContentBackfill) bool {
			return processContentBackfill(ctx, contentExtractor, article, queue)
		},
		hashOf:       func(a database.ArticleForContentBackfill) string { return a.URLHash },
		bumpAttempts: db.BumpBackfillAttempts,
	})

	// Flush whatever was queued even on error/cancel — same rationale as the
	// image backfill: partial progress beats none, and unflushed articles
	// resurface in the next run's candidate set naturally.
	if flushErr := flushContentUpdates(db, pending); flushErr != nil {
		logger.Warnf("Failed to flush content updates (%d queued): %v", len(pending), flushErr)
	} else if len(pending) > 0 {
		logger.Infof("Flushed %d content updates", len(pending))
	}

	return err
}

// processContentBackfill uses go-readability to extract the main text content
// from a single article's webpage. If valid content is extracted, enqueues
// a ContentUpdate for the caller to batch-write later. Returns true if an
// update was queued.
//
// Applies parser.SanitizeContent with the per-source-aware effective cap so
// backfilled content matches what initial-parse writes would have stored —
// without this clamp the backfill path could silently insert arbitrarily
// large content from go-readability, bypassing the parser's defences.
func processContentBackfill(ctx context.Context, contentExtractor *parser.ContentExtractor, article database.ArticleForContentBackfill, queue func(database.ContentUpdate)) bool {
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

	var perSourceCap *int
	if article.Source != nil {
		perSourceCap = article.Source.MaxContentLength
	}
	content = parser.SanitizeContent(content, parser.EffectiveContentCap(perSourceCap))

	queue(database.ContentUpdate{
		URLHash: article.URLHash,
		Content: content,
	})
	articleLog.Info("backfill_queued", "chars", len(content))
	return true
}

// flushContentUpdates writes the queued content updates via
// BatchUpdateArticleContent in chunks of contentFlushBatchSize. Per-batch
// errors are accumulated and returned together so one bad batch doesn't
// drop the rest.
func flushContentUpdates(db Store, updates []database.ContentUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	var errs []error
	for i := 0; i < len(updates); i += contentFlushBatchSize {
		end := i + contentFlushBatchSize
		if end > len(updates) {
			end = len(updates)
		}
		if err := db.BatchUpdateArticleContent(updates[i:end]); err != nil {
			errs = append(errs, fmt.Errorf("batch %d-%d: %w", i, end, err))
		}
	}
	return errors.Join(errs...)
}
