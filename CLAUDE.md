# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Pulse Backend is a self-hosted news aggregation backend for the Pulse iOS app. It uses Go for RSS fetching and Supabase (PostgreSQL) for database and auto-generated REST API.

**Tech Stack:** Go 1.24 | Supabase | GitHub Actions | PostgreSQL

## Architecture

```
GitHub Actions (every 2 hours)
    ↓
Go RSS Worker (rss-worker/)
    ├─ Fetch RSS feeds (133 sources, adaptive intervals)
    ├─ Parse with gofeed library
    ├─ Enrich: og:image extraction (5 workers)
    ├─ Enrich: content extraction (3 workers)
    ├─ Extract: media enclosures (audio/video URLs, duration)
    └─ Batch insert to Supabase (50/batch, dedup via url_hash)
        ↓
PostgreSQL (articles, sources, categories, fetch_logs)
        ↓
Edge Functions (caching proxy + in-memory cache)
    ├── /api-categories  → Cache: 24h + 1h memory
    ├── /api-sources     → Cache: 1h + 30min memory
    ├── /api-articles    → Cache: 5min + ETag
    ├── /api-search      → Cache: 1min (private)
    └── /api-health      → Cache: no-store
        ↓
Pulse iOS App
```

## Build and Run Commands

Use `make help` to see all available commands. Key commands:

```bash
# Set environment variables (required for run commands)
export SUPABASE_URL="https://your-project.supabase.co"
export SUPABASE_SERVICE_ROLE_KEY="your-service-role-key"

# Build & Run
make build             # Build the RSS worker binary
make run               # Run the RSS worker (fetch feeds)
make cleanup           # Remove articles older than 30 days
make backfill-images   # Fetch og:images for articles missing images
make backfill-content  # Extract full content for articles

# Testing
make test              # Run all tests (Go + Deno)
make test-go           # Run Go tests
make test-go-cover     # Run Go tests with coverage
make test-go-race      # Run Go tests with race detector
make test-deno         # Run Deno Edge Function tests

# Supabase Functions
make deploy            # Deploy all Edge Functions
make functions-serve   # Run Edge Functions locally

# Without Make:
cd rss-worker && go run .
cd rss-worker && go run . cleanup
cd rss-worker && go test -v ./...
```

## Project Structure

```
pulse-backend/
├── Makefile                           # Common commands (make help)
├── rss-worker/                        # Main Go application
│   ├── main.go                        # Entry point with command routing
│   └── internal/
│       ├── config/
│       │   ├── config.go              # Env config (SUPABASE_URL, SUPABASE_SERVICE_ROLE_KEY)
│       │   └── config_test.go         # Config tests (100% coverage)
│       ├── models/
│       │   ├── models.go              # Data models (Article, Source, Category, FetchLog)
│       │   └── models_test.go         # Models tests (100% coverage)
│       ├── parser/
│       │   ├── parser.go              # RSS parsing with gofeed + enrichment orchestration
│       │   ├── parser_test.go         # Parser helper tests
│       │   ├── ogimage.go             # og:image meta tag extraction
│       │   ├── ogimage_test.go        # OG image tests with httptest
│       │   ├── content.go             # Full article content extraction (go-readability)
│       │   └── content_test.go        # Content extraction tests
│       ├── database/
│       │   ├── supabase.go            # Supabase REST API client (with retry logic)
│       │   └── supabase_test.go       # Database client tests
│       ├── httputil/
│       │   ├── transport.go           # Shared HTTP transport + RateLimitingTransport (per-host token bucket)
│       │   └── transport_test.go      # Transport tests
│       └── logger/
│           ├── logger.go              # slog-backed logger (LOG_FORMAT=text|json)
│           └── logger_test.go         # Logger tests
├── supabase/
│   ├── migrations/
│   │   ├── 001_initial_schema.sql     # Core database schema
│   │   ├── 002_add_media_support.sql  # Podcast/video media fields
│   │   ├── 003_add_podcast_video_sources.sql  # 34 curated podcast/video sources
│   │   ├── 004_update_articles_with_source_view.sql  # Expose media fields in API view
│   │   ├── 005_fix_security_issues.sql  # Harden RLS, view, function security
│   │   ├── 006_add_composite_indexes.sql  # Composite indexes for performance
│   │   ├── 007_add_language_support.sql   # Language column on sources & articles
│   │   ├── 008_add_pt_es_sources.sql     # Portuguese & Spanish RSS sources
│   │   ├── 009_add_more_pt_es_sources.sql  # More PT & ES sources
│   │   ├── 010_add_pt_es_podcasts_videos.sql  # PT & ES podcasts, videos, politics
│   │   ├── 011_revoke_cleanup_from_anon.sql   # Restrict cleanup function access
│   │   ├── 012_add_content_to_search_vector.sql  # Include content in full-text search
│   │   ├── 013_drop_fetch_interval_minutes.sql   # Remove unused column
│   │   ├── 014_add_batch_image_update_rpc.sql    # RPC for batch image updates
│   │   ├── 015_add_fetch_interval_hours.sql      # Adaptive fetch frequency
│   │   ├── 016_denormalize_articles.sql          # Denormalize source/category into articles
│   │   ├── 017_backfill_denormalized_articles.sql # Backfill denormalized columns
│   │   └── 018_add_backfill_tracking.sql  # Attempt counters + cooldown RPC for backfills
│   └── functions/                     # Edge Functions (caching proxy)
│       ├── _shared/                   # Shared utilities
│       │   ├── cors.ts                # CORS headers
│       │   ├── cors_test.ts           # CORS tests
│       │   ├── cache.ts               # Cache-Control utilities
│       │   ├── cache_test.ts          # Cache tests
│       │   ├── etag.ts                # ETag generation
│       │   ├── etag_test.ts           # ETag tests
│       │   ├── memory-cache.ts        # In-memory TTL cache
│       │   ├── memory-cache_test.ts   # Memory cache tests
│       │   └── supabase-proxy.ts      # Proxy logic
│       ├── api-categories/index.ts    # Categories endpoint (24h cache)
│       ├── api-sources/index.ts       # Sources endpoint (1h cache)
│       ├── api-articles/index.ts      # Articles endpoint (5min + ETag)
│       ├── api-search/index.ts        # Search endpoint (1min private)
│       └── api-health/index.ts        # Health check endpoint (no-store)
├── .github/workflows/
│   ├── fetch-rss.yml                  # Runs every 2 hours
│   ├── cleanup.yml                    # Runs daily at 3 AM UTC
│   ├── test.yml                       # Unit tests + lint + govulncheck on push/PR
│   ├── security.yml                   # Secret scan, SAST, deps, SBOM (push/PR + weekly)
│   └── deploy-functions.yml           # Auto-deploy Edge Functions on push
└── docs/ios-integration.md            # iOS app integration guide
```

## Key Components

### Main Entry Point (`main.go`)
- Command routing: default fetch, `cleanup`, `backfill-images`, `backfill-content`
- Concurrent source processing with semaphore (default: 5 concurrent)
- Fetch logging to `fetch_logs` table

### HTTP Utilities (`internal/httputil/`)
- `transport.go`: Shared `http.Transport` with tuned connection pooling (`MaxIdleConnsPerHost: 10`, HTTP/2 enabled). Clients build on this via:
  - `httputil.NewClient(timeout)` — plain shared-transport client
  - `httputil.NewClientWithRedirectLimit(timeout, maxRedirects)` — adds a redirect cap
  - `httputil.NewRateLimitedClient(timeout, rps, burst, maxRedirects)` — wraps the shared transport with a `RateLimitingTransport` (per-host token bucket from `golang.org/x/time/rate`). Used by the RSS, og:image, and content clients; Supabase traffic deliberately uses the plain client.

### Parser Module (`internal/parser/`)
- `parser.go`: Orchestrates RSS parsing via `mmcdole/gofeed`, then enriches articles with og:images (5 workers) and content (3 workers). Also extracts media enclosures (audio/video) for podcasts and videos. Package-level `hostRPS`/`hostBurst` vars are set via `parser.SetHostRateLimit(rps, burst)` at startup (from `main()`); subsequent `New()`, `NewOGImageExtractor()`, and `NewContentExtractor()` pick up the override. Defaults are `DefaultHostRPS=2.0`, `DefaultHostBurst=5`.
- `ogimage.go`: Extracts og:image/twitter:image from article HTML `<head>` (100KB limit, byte-based regex matching)
- `content.go`: Uses `go-shiori/go-readability` for article text extraction (5MB response limit)
- Media extraction: Parses audio/video enclosures and iTunes duration from RSS feeds

### Database Client (`internal/database/supabase.go`)
- Direct HTTP calls to Supabase REST API (uses shared HTTP transport)
- Retry with exponential backoff on 429/502/503/504 (up to 3 retries)
- Batch article inserts: POST arrays of 50 with `on_conflict=url_hash` + `ignore-duplicates`
- Batch image updates: `batch_update_article_images` RPC for og:image updates on duplicates
- Batch source updates: `UpdateSourcesLastFetched()` with PostgREST `in` filter
- Adaptive fetch: `GetActiveSources()` filters by `fetch_interval_hours` and `last_fetched_at`
- Key methods: `GetActiveSources()`, `InsertArticles()`, `BatchUpdateArticleImages()`, `UpdateSourcesLastFetched()`, `CleanupOldArticles()`, `GetArticlesNeedingOGImage(limit, maxAttempts, cooldownHours)`, `GetArticlesNeedingContent(limit, maxAttempts, cooldownHours)`, `BumpBackfillAttempts(urlHashes, kind)`

### Data Models (`internal/models/models.go`)
- `Source` struct with `FetchIntervalHours`, `EmbeddedCategory`, `ShouldFetch()` method
- `Article` struct with media fields and denormalized `SourceName`, `SourceSlug`, `CategoryName`, `CategorySlug`
- `NewArticle()` accepts language parameter — articles inherit language from their source
- `HashURL()` function for SHA256-based URL deduplication
- `FetchResult` for concurrent processing results

### Edge Functions (`supabase/functions/`)
Caching proxy layer for iOS app with Cache-Control headers:

| Endpoint | Cache | Description |
|----------|-------|-------------|
| `/api-categories` | 24h public | Static category list |
| `/api-sources` | 1h public | RSS source list |
| `/api-articles` | 5min + ETag | Article feed with 304 support |
| `/api-search` | 1min private | Full-text search via RPC |
| `/api-health` | no-store | Health check (status + timestamp) |

**Deployment:**
```bash
# Install Supabase CLI if needed
brew install supabase/tap/supabase

# Deploy all functions
supabase functions deploy

# Or deploy individually
supabase functions deploy api-categories
supabase functions deploy api-sources
supabase functions deploy api-articles
supabase functions deploy api-search
```

**Local testing:**
```bash
supabase start
supabase functions serve
curl -i http://localhost:54321/functions/v1/api-articles?limit=5
```

## Database Schema

Tables: `categories` (10, including Podcasts & Videos), `sources` (133 pre-configured), `articles` (with full-text search via tsvector and media fields), `fetch_logs`

Language support:
- `sources.language`: ISO 639-1 code (VARCHAR(5), default `'en'`), e.g. `'en'`, `'pt'`, `'es'`
- `articles.language`: Inherited from source at insert time, indexed for filtering

Article media fields (for podcasts/videos):
- `media_type`: 'podcast' or 'video'
- `media_url`: Direct URL to audio/video file
- `media_duration`: Duration in seconds
- `media_mime_type`: MIME type (audio/mpeg, video/mp4, etc.)

Denormalized fields (avoids JOINs):
- `source_name`, `source_slug`: From sources table
- `category_name`, `category_slug`: From categories table

Source adaptive fetch:
- `fetch_interval_hours`: Default 2, podcasts/videos set to 6

Backfill tracking (migration 018):
- `image_backfill_attempts`, `image_backfill_last_attempt_at`
- `content_backfill_attempts`, `content_backfill_last_attempt_at`
- Backfill queries exclude articles that exhausted `BACKFILL_MAX_ATTEMPTS` or whose last attempt was within `BACKFILL_COOLDOWN_HOURS`. Successful extractions leave the candidate set naturally (image_url/content becomes non-null).

Key functions:
- `cleanup_old_articles(days_to_keep)` - Called by cleanup command
- `search_articles(search_query, result_limit)` - Full-text search
- `batch_update_article_images(updates)` - Batch image URL updates
- `bump_backfill_attempts(url_hashes, kind)` - Increments attempt counter + stamps last_attempt_at; `kind` is "image" or "content"

View: `articles_with_source` - Simple SELECT from articles (no JOINs after denormalization)

## Configuration

Environment variables:
- `SUPABASE_URL` - Required
- `SUPABASE_SERVICE_ROLE_KEY` - Required (keep secret, needed for writes)
- `LOG_LEVEL` - Optional: DEBUG, INFO (default), WARN, ERROR
- `LOG_FORMAT` - Optional: `text` (default, slog TextHandler) or `json` (slog JSONHandler for log aggregators)
- `HOST_RATE_LIMIT_RPS` - Optional: per-host requests/sec for RSS/og:image/content HTTP clients (default `2.0`). Supabase traffic is not throttled.
- `HOST_RATE_LIMIT_BURST` - Optional: per-host burst allowance (default `5`)
- `BACKFILL_MAX_ATTEMPTS` - Optional: max retries per article before it's excluded from backfill (default `3`)
- `BACKFILL_COOLDOWN_HOURS` - Optional: min gap between backfill attempts on the same article (default `24`)

Defaults in `internal/config/config.go`:
- `MaxConcurrent`: 5 sources processed simultaneously
- `ArticleRetentionDays`: 30 days

Graceful shutdown: the worker installs a `signal.NotifyContext` handler for
SIGINT/SIGTERM at startup. In-flight goroutines check `ctx.Done()` and HTTP
requests cancel via request context, so GitHub Actions cancellations and
runner rotations exit without orphaning batches.

## Testing

Unit tests cover Go packages and Deno Edge Functions:

| Package | Coverage | Key Tests |
|---------|----------|-----------|
| `internal/models` | 100% | HashURL, NewArticle, ShouldFetch, CategoryName |
| `internal/config` | 100% | Load + env var validation (including HOST_RATE_LIMIT_* and BACKFILL_*) |
| `internal/httputil` | 92% | SharedTransport, NewClient, NewClientWithRedirectLimit, RateLimitingTransport (per-host serialization, cross-host independence, ctx-cancel short-circuit) |
| `internal/parser` | 92% | cleanHTML, extractImageURL, OG image, content extraction, itemToArticle |
| `internal/database` | 82% | Batch inserts, batch image RPC, batch source updates, retry logic, BumpBackfillAttempts |
| `internal/logger` | 86% | Level filtering, text + JSON output format, `With()` field propagation |
| `main` | 74% | processSource, runFetch, processOGImageBackfill, processContentBackfill, runBackfill (with attempt-bump) |
| `_shared/*.ts` | — | cache, cors, etag utilities |

Run tests:
```bash
make test           # All tests
make test-go-cover  # Go with coverage report
make test-deno      # Deno Edge Function tests
```

## GitHub Actions

- **fetch-rss.yml**: Every 2 hours + manual trigger
- **cleanup.yml**: Daily at 3 AM UTC + manual trigger
- **test.yml**: Runs on push/PR to main (Go tests, lint, govulncheck, Deno tests)
- **security.yml**: Runs on push/PR to main + weekly (Mon 06:00 UTC). Jobs: secret scan (gitleaks + TruffleHog), Go SAST (gosec), govulncheck, Trivy filesystem scan (vuln/secret/misconfig), CycloneDX SBOM artifact
- **deploy-functions.yml**: Auto-deploys Edge Functions on push to main

Secrets needed: `SUPABASE_URL`, `SUPABASE_SERVICE_ROLE_KEY`, `SUPABASE_ACCESS_TOKEN`, `SUPABASE_PROJECT_REF`

## Monitoring

Check `fetch_logs` table in Supabase Table Editor for:
- `status`: running / completed / partial_failure / failed
- `articles_inserted`, `articles_skipped`, `errors`

GitHub Actions logs: Repository → Actions tab
