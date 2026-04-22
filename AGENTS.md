# AGENTS.md

This file provides guidance to AI coding agents when working with this repository.

## Project Overview

Pulse Backend is a self-hosted news aggregation backend for the Pulse iOS app. It uses Go for RSS fetching and Supabase (PostgreSQL) for database and auto-generated REST API.

**Tech Stack:** Go 1.24 | Supabase | GitHub Actions | PostgreSQL | Deno (Edge Functions)

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
    ├── /api-categories    → Cache: 24h + 1h memory
    ├── /api-sources       → Cache: 1h + 30min memory
    ├── /api-articles      → Cache: 5min + ETag
    ├── /api-search        → Cache: 1min (private)
    ├── /api-health        → Cache: no-store
    └── /api-source-health → Cache: 60s (feed health + summary)
        ↓
Pulse iOS App
```

## Common Commands

Use `make help` to see all available commands:

```bash
# Testing
make test              # Run all tests (Go + Deno)
make test-go           # Run Go tests
make test-go-cover     # Run Go tests with coverage
make test-go-race      # Run Go tests with race detector
make test-deno         # Run Deno Edge Function tests

# Build & Run (requires SUPABASE_URL and SUPABASE_SERVICE_ROLE_KEY env vars)
make build             # Build the RSS worker binary
make run               # Run the RSS worker (fetch feeds)
make cleanup           # Remove articles older than 30 days
make backfill-images   # Fetch og:images for articles missing images
make backfill-content  # Extract full content for articles

# Supabase Functions
make deploy            # Deploy all Edge Functions
make functions-serve   # Run Edge Functions locally

# Utilities
make clean             # Remove build artifacts
```

## Project Structure

```
pulse-backend/
├── Makefile                           # Common commands (make help)
├── rss-worker/                        # Main Go application
│   ├── main.go                        # Entry point with command routing
│   └── internal/
│       ├── config/
│       │   ├── config.go              # Env config
│       │   └── config_test.go         # Tests (100% coverage)
│       ├── models/
│       │   ├── models.go              # Data models (Article, Source, Category, FetchLog)
│       │   └── models_test.go         # Tests (100% coverage)
│       ├── parser/
│       │   ├── parser.go              # RSS parsing + enrichment orchestration
│       │   ├── parser_test.go         # Parser helper tests
│       │   ├── ogimage.go             # og:image meta tag extraction
│       │   ├── ogimage_test.go        # OG image tests
│       │   ├── content.go             # Article content extraction (go-readability)
│       │   └── content_test.go        # Content extraction tests
│       ├── database/
│       │   ├── supabase.go            # Supabase REST API client (with retry logic)
│       │   └── supabase_test.go       # Database client tests
│       ├── httputil/
│       │   └── transport.go           # Shared HTTP transport with connection pooling
│       └── logger/
│           ├── logger.go              # Structured logging with level support
│           └── logger_test.go         # Logger tests
├── supabase/
│   ├── migrations/
│   │   ├── 001_initial_schema.sql     # Core database schema
│   │   ├── 002_add_media_support.sql  # Podcast/video media fields
│   │   ├── 003_add_podcast_video_sources.sql  # Curated sources
│   │   ├── 004_update_articles_with_source_view.sql  # Expose media fields in API
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
│   │   ├── 018_add_backfill_tracking.sql         # Attempt counters + cooldown RPC for backfills
│   │   ├── 019_add_source_fetch_state_columns.sql # etag, last_modified, consecutive_failures, circuit_open_until on sources
│   │   └── 020_add_source_health_infra.sql        # batch_update_source_fetch_state RPC + source_health view
│   └── functions/                     # Edge Functions (Deno/TypeScript)
│       ├── _shared/                   # Shared utilities
│       │   ├── cors.ts / cors_test.ts
│       │   ├── cache.ts / cache_test.ts
│       │   ├── etag.ts / etag_test.ts
│       │   ├── memory-cache.ts / memory-cache_test.ts
│       │   └── supabase-proxy.ts
│       ├── api-categories/index.ts    # Categories endpoint (24h cache)
│       ├── api-sources/index.ts       # Sources endpoint (1h cache)
│       ├── api-articles/index.ts      # Articles endpoint (5min + ETag)
│       ├── api-search/index.ts        # Search endpoint (1min private)
│       ├── api-health/index.ts        # Health check endpoint (no-store)
│       └── api-source-health/index.ts # Per-source fetch health + summary (60s cache)
├── .github/
│   ├── workflows/
│   │   ├── fetch-rss.yml              # Runs every 2 hours
│   │   ├── cleanup.yml                # Runs daily at 3 AM UTC
│   │   ├── test.yml                   # Unit tests + lint + govulncheck on push/PR
│   │   ├── security.yml               # Secret scan, SAST, deps, SBOM (push/PR + weekly)
│   │   ├── pr-checks.yml              # PR-only: title conventional-commits, go.mod sync, migration format
│   │   ├── deploy-functions.yml       # Auto-deploy Edge Functions on push
│   │   └── watchdog.yml               # Source health check every 6h (fails job on degradation)
│   └── dependabot.yml                 # Weekly dependency updates
└── docs/ios-integration.md            # iOS app integration guide
```

## Key Components

### Go RSS Worker (`rss-worker/`)

| Component | File | Description |
|-----------|------|-------------|
| Entry Point | `main.go` | Command routing: fetch, cleanup, backfill-images, backfill-content. `main()` wraps context with `signal.NotifyContext(SIGINT, SIGTERM)` for graceful shutdown; emits a `run_id` on every run |
| Config | `internal/config/config.go` | Loads SUPABASE_URL, SUPABASE_SERVICE_ROLE_KEY, plus optional LOG_FORMAT, HOST_RATE_LIMIT_RPS/BURST, BACKFILL_MAX_ATTEMPTS/COOLDOWN_HOURS |
| Models | `internal/models/models.go` | Article (with media + denormalized fields), Source (with EmbeddedCategory, ShouldFetch()), FetchLog; HashURL() for dedup |
| Parser | `internal/parser/parser.go` | RSS parsing via gofeed + parallel enrichment + media extraction. `SetHostRateLimit(rps, burst)` overrides per-host defaults (2.0 rps, burst 5) for all HTTP clients built afterward |
| OG Image | `internal/parser/ogimage.go` | Extracts og:image from article HTML (100KB limit) |
| Content | `internal/parser/content.go` | Extracts article text via go-readability |
| Database | `internal/database/supabase.go` | Supabase REST API client with batch inserts (50/batch), batch image RPC, retry logic (exponential backoff on 429/5xx). `GetActiveSources()` filters by `fetch_interval_hours` and `circuit_open_until`. `BatchUpdateSourceFetchState()` persists per-source etag/last_modified/consecutive_failures/circuit_open_until via the `batch_update_source_fetch_state` RPC (migration 020). Backfill queries take `(limit, maxAttempts, cooldownHours)`; `BumpBackfillAttempts(urlHashes, kind)` marks failed attempts |
| HTTP Utils | `internal/httputil/transport.go` | Shared HTTP transport with tuned connection pooling; `NewClient`, `NewClientWithRedirectLimit`, and `NewRateLimitedClient` (wraps `RateLimitingTransport` with per-host token bucket from `golang.org/x/time/rate`) |
| Logger | `internal/logger/logger.go` | slog-backed: LOG_LEVEL gates emission; LOG_FORMAT=text (default, slog.TextHandler) or json (slog.JSONHandler). Printf-style `Debugf/Infof/Warnf/Errorf/Fatalf` plus `With(k, v, ...)` returning a sub-*slog.Logger for structured correlation |

### Edge Functions (`supabase/functions/`)

| Endpoint | Cache | Description |
|----------|-------|-------------|
| `/api-categories` | 24h public | Static category list |
| `/api-sources` | 1h public | RSS source list |
| `/api-articles` | 5min + ETag | Article feed with 304 support |
| `/api-search` | 1min private | Full-text search via RPC |
| `/api-health` | no-store | Health check (status + timestamp) |
| `/api-source-health` | 60s public | Per-source fetch health + aggregate summary; watchdog.yml polls this |

## Testing

Tests use Go's standard testing package with `httptest` for mocking HTTP calls, and Deno's built-in test runner for Edge Functions.

| Package | Coverage | Description |
|---------|----------|-------------|
| `internal/models` | 100% | HashURL, NewArticle, ShouldFetch, CategoryName |
| `internal/config` | 100% | Env var loading + defaults (HOST_RATE_LIMIT_*, BACKFILL_*, CIRCUIT_*) |
| `internal/httputil` | 92% | SharedTransport, NewClient, NewClientWithRedirectLimit, RateLimitingTransport (per-host serialization, cross-host independence, ctx-cancel short-circuit) |
| `internal/parser` | 92% | HTML cleaning, image extraction, OG/content fetching, itemToArticle, ParseFeed (200/304/non-2xx + conditional-GET headers) |
| `internal/database` | 82% | Batch inserts, batch image RPC, BatchUpdateSourceFetchState (payload shape, empty noop, 5xx), GetActiveSources circuit filter, retry logic, BumpBackfillAttempts |
| `internal/logger` | 86% | Level filtering, text + JSON output, `With()` field propagation (thread-safe via atomic.Pointer) |
| `main` | 76% | processSource, runFetch, nextCircuitOpenUntil (threshold/exponential/cap/overflow), buildSourceFetchState (success resets, failure preserves ETag + trips circuit, 304 is success), runBackfill |
| `_shared/*.ts` | — | Cache, CORS, ETag, memory cache utilities |

Run tests before committing:
```bash
make test
```

## Configuration

Required environment variables:
- `SUPABASE_URL` - Supabase project URL
- `SUPABASE_SERVICE_ROLE_KEY` - Service role key (keep secret, needed for writes)

Optional environment variables:
- `LOG_LEVEL` - DEBUG, INFO (default), WARN, ERROR
- `LOG_FORMAT` - `text` (default, slog TextHandler) or `json` (slog JSONHandler for log aggregators)
- `HOST_RATE_LIMIT_RPS` - per-host requests/sec for RSS/og:image/content HTTP clients (default `2.0`). Supabase traffic is not throttled.
- `HOST_RATE_LIMIT_BURST` - per-host burst allowance (default `5`)
- `BACKFILL_MAX_ATTEMPTS` - max retries per article before it's excluded from backfill (default `3`)
- `BACKFILL_COOLDOWN_HOURS` - min gap between backfill attempts on the same article (default `24`)
- `CIRCUIT_FAILURE_THRESHOLD` - consecutive fetch failures before the circuit trips (default `5`)
- `CIRCUIT_BASE_BACKOFF_HOURS` - initial cool-off window on trip; doubles per additional failure (default `1`)
- `CIRCUIT_MAX_BACKOFF_HOURS` - cap on the exponential circuit backoff (default `24`)

Defaults in `internal/config/config.go`:
- `MaxConcurrent`: 5 sources processed simultaneously
- `ArticleRetentionDays`: 30 days

Graceful shutdown: `main()` installs `signal.NotifyContext(SIGINT, SIGTERM)` and threads that baseCtx into every run* command, so GHA cancellations and runner rotations exit cleanly instead of orphaning batches.

## Database Schema

Tables:
- `categories` - 10 categories (including Podcasts & Videos)
- `sources` - 133 pre-configured feeds with `fetch_interval_hours` (default 2, podcasts/videos 6). Migration 019 adds `etag`/`last_modified` (conditional GET validators) and `consecutive_failures`/`circuit_open_until` (circuit breaker state).
- `articles` - News articles with full-text search (tsvector), media fields, denormalized source/category columns, and backfill tracking (`image_backfill_attempts`, `image_backfill_last_attempt_at`, `content_backfill_attempts`, `content_backfill_last_attempt_at` — migration 018)
- `fetch_logs` - Monitoring records

Key functions:
- `cleanup_old_articles(days_to_keep)` - Remove old articles
- `search_articles(search_query, result_limit)` - Full-text search
- `batch_update_article_images(updates)` - Batch image URL updates
- `bump_backfill_attempts(url_hashes, kind)` - Increments attempt counter + stamps `last_attempt_at`; `kind` is `"image"` or `"content"` (migration 018)
- `batch_update_source_fetch_state(updates)` - One round-trip per fetch cycle; JSONB array of per-source state (etag, last_modified, consecutive_failures, circuit_open_until, last_fetched_at) — migration 020

Views:
- `articles_with_source` - Simple SELECT from articles (no JOINs after denormalization)
- `source_health` - Per-source health snapshot (circuit_open, consecutive_failures, most_recent_article_at, articles_last_24h); `security_invoker=on`. Powers `api-source-health` and the watchdog workflow.

## Code Style Guidelines

- Go code follows standard Go conventions (`go fmt`, `go vet`)
- Use table-driven tests for comprehensive coverage
- HTTP calls should be mocked with `httptest.Server` in tests
- New HTTP clients must use `httputil.NewClient`, `httputil.NewClientWithRedirectLimit`, or `httputil.NewRateLimitedClient` (preferred for external hosts) to share the connection pool
- Prefer `logger.With(key, val, ...)` at per-source/per-article sites for structured correlation; the printf-style `logger.Infof` is fine for one-off summary lines
- Edge Functions use TypeScript with Deno
- All new code should include tests

## GitHub Actions

| Workflow | Schedule | Description |
|----------|----------|-------------|
| `fetch-rss.yml` | Every 2 hours | Fetch RSS feeds |
| `cleanup.yml` | Daily 3 AM UTC | Remove old articles |
| `test.yml` | On push/PR | Go tests (race + coverage), golangci-lint, govulncheck, Deno tests |
| `security.yml` | On push/PR + weekly Mon 06:00 UTC | gitleaks + TruffleHog (secrets), gosec (Go SAST), govulncheck, Trivy (deps/secrets/misconfig), CycloneDX SBOM |
| `pr-checks.yml` | On PR to master only | PR title conventional-commits, go.mod Sync (`go mod tidy` must be a no-op), Migration Format (NNN_*.sql, no gaps, no duplicate prefixes) |
| `deploy-functions.yml` | On push to master | Auto-deploy Edge Functions |
| `watchdog.yml` | Every 6 hours + manual | Polls `api-source-health`; fails job (→ GitHub email) when circuit/stale/high-failure counts exceed thresholds |

Branch protection on `master` requires all 11 checks across `test.yml`, `security.yml`, and `pr-checks.yml` to pass before merge. Direct pushes to `master` are blocked (including for admins); every change goes through a PR. Merge strategy is squash-only with `delete_branch_on_merge` enabled.

Secrets needed: `SUPABASE_URL`, `SUPABASE_SERVICE_ROLE_KEY`, `SUPABASE_ACCESS_TOKEN`, `SUPABASE_PROJECT_REF`
