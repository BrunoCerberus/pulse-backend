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
    ├── /api-categories    → Cache: 24h + 1h memory
    ├── /api-sources       → Cache: 1h + 30min memory
    ├── /api-articles      → Cache: 5min + ETag
    ├── /api-search        → Cache: 1min (private)
    ├── /api-health        → Cache: no-store
    └── /api-source-health → Cache: 60s (feed health + summary)
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
make cleanup           # Remove articles older than 7 days (and same-age fetch_logs)
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
│       │   ├── transport.go           # SharedTransport (Supabase) + SafeTransport (SSRF-aware DialContext, used by user-content clients) + RateLimitingTransport (per-host token bucket); IsForbiddenIP / ValidateSSRFTarget / SecureDialContext guards
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
│   │   ├── 018_add_backfill_tracking.sql  # Attempt counters + cooldown RPC for backfills
│   │   ├── 019_add_source_fetch_state_columns.sql # etag, last_modified, consecutive_failures, circuit_open_until on sources
│   │   ├── 020_add_source_health_infra.sql    # batch_update_source_fetch_state RPC + source_health view
│   │   ├── 021_batch_cleanup_old_articles.sql # Batch cleanup_old_articles + raise per-function statement_timeout
│   │   ├── 022_add_db_size_rpc.sql            # get_db_size_bytes RPC for DB-size watchdog
│   │   ├── 023_inactivate_dead_sources.sql    # Data cleanup: flip is_active=false on long-dead/never-produced sources
│   │   ├── 024_strip_content_from_search_vector.sql # Drop content from search_vector to shrink GIN index
│   │   ├── 025_drop_unused_indexes.sql        # Drop indexes with idx_scan=0 to cut write amplification
│   │   ├── 026_add_batch_content_update_rpc.sql # Batch content updates RPC for backfill
│   │   ├── 027_security_hardening.sql         # Audit-driven hardening: explicit search_articles projection + 200-char cap + 3s timeout; SECURITY DEFINER funcs rebuilt with `search_path = ''` + in-function role check; column-level GRANT on articles; articles_with_source recreated; source_health + get_db_size_bytes revoked from anon
│   │   ├── 028_search_articles_explicit_casts.sql # Hotfix consolidation: replace pg_catalog.least with bare LEAST + add ::TEXT casts on VARCHAR(N) cols in search_articles RETURNS TABLE
│   │   └── 029_compress_articles_content_lz4.sql # Switch articles.content TOAST compression from pglz to LZ4 (PG14+ build option); new writes only — existing rows rewrite via 7d cleanup cycle, no VACUUM FULL
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
│       ├── api-health/index.ts        # Health check endpoint (no-store)
│       └── api-source-health/index.ts # Per-source fetch health + summary + DB size (60s cache)
├── .github/
│   ├── workflows/
│   │   ├── fetch-rss.yml              # Runs every 2 hours
│   │   ├── cleanup.yml                # Runs daily at 3 AM UTC
│   │   ├── backfill.yml               # og:image + content backfill daily at 04:30 UTC
│   │   ├── test.yml                   # Unit tests + lint + govulncheck on push/PR
│   │   ├── security.yml               # Secret scan, SAST, deps, SBOM (push/PR + weekly)
│   │   ├── pr-checks.yml              # PR-only: title conventional-commits, go.mod sync, migration format
│   │   ├── deploy-functions.yml       # Auto-deploy Edge Functions on push
│   │   ├── watchdog.yml               # Source health check every 6h (fails job on degradation)
│   │   ├── lgpd-conformance.yml       # LGPD guard rails (PII bans, doc gates, ops + structural)
│   │   └── gdpr-conformance.yml       # GDPR + CCPA guard rails (same shape, EU/US patterns)
│   ├── lgpd-gdpr-rules.toml           # Custom gitleaks rules: CPF, CNPJ, IBAN, US SSN
│   └── pii-allowlist.txt              # Allowed email literals (maintainer + RFC 6761 reserved)
└── docs/
    ├── api-reference.md               # Edge Function endpoints + request guards
    ├── database-schema.md             # Schema reference
    ├── ios-integration.md             # iOS app integration guide
    ├── operations-runbook.md          # Day-2 ops + on-call notes
    ├── privacy.md                     # Overall privacy posture (no end-user PII)
    ├── lgpd-conformance.md            # LGPD (Brazil) — position + guard rails
    ├── gdpr-conformance.md            # GDPR (EU) — position + guard rails
    ├── ccpa-conformance.md            # CCPA / CPRA (California) — position + guard rails
    ├── ropa.md                        # Record of Processing Activities
    └── data-retention.md              # 7-day retention policy
```

## Key Components

### Main Entry Point (`main.go`)
- Command routing: default fetch, `cleanup`, `backfill-images`, `backfill-content`
- Concurrent source processing with semaphore (default: 5 concurrent)
- Fetch logging to `fetch_logs` table

### HTTP Utilities (`internal/httputil/`)
- `transport.go`: Two base transports, both with tuned connection pooling (`MaxIdleConnsPerHost: 10`, HTTP/2 enabled):
  - `SharedTransport` — used by the Supabase client (single trusted host).
  - `SafeTransport` — used by every user-content client. Its `DialContext` resolves the host once, rejects forbidden IPs (`IsForbiddenIP`: loopback / RFC 1918 / link-local 169.254/16 / multicast / unspecified), then dials the resolved IP literal so a hostile DNS server can't rebind between check and connect.
- Client constructors:
  - `httputil.NewClient(timeout)` — `SharedTransport`, no redirect cap.
  - `httputil.NewClientWithRedirectLimit(timeout, maxRedirects)` — `SharedTransport` + redirect cap.
  - `httputil.NewRateLimitedClient(timeout, rps, burst, maxRedirects)` — wraps `SafeTransport` with `RateLimitingTransport` (per-host token bucket from `golang.org/x/time/rate`) and a redirect-time SSRF re-validation. Used by RSS / og:image / content clients.
- Test-only knob: `SetAllowLoopback(bool)` exempts 127.0.0.1 / ::1 from the SSRF check so `httptest.Server` works. Each affected test package calls it via `TestMain`; production never touches it.

### Parser Module (`internal/parser/`)
- `parser.go`: Orchestrates RSS parsing via `mmcdole/gofeed`, then enriches articles with og:images (5 workers) and content (3 workers). Also extracts media enclosures (audio/video) for podcasts and videos. Each `ParseFeed` allocates a fresh `gofeed.Parser` with explicit translator fields to avoid the lazy-init race. Feed body is wrapped in `io.LimitReader(50 MB)` to defend against memory exhaustion from a hostile publisher. `itemToArticle` rejects non-`http(s)` URLs (article + media + thumbnail), canonicalizes the article URL (drop fragment, lowercase scheme/host, sort query), clamps `published_at` to `[10y ago, now+1h]`, decodes HTML entities via `html.UnescapeString` (catches `&#x3c;` numeric escapes the old replacer missed), strips C0/C1 control characters and bidi-override codepoints, and applies length caps (title 500 / summary 4096 / content 200K / author 200 / URL 2048). Package-level `hostRPS`/`hostBurst` vars are set via `parser.SetHostRateLimit(rps, burst)` at startup (from `main()`); subsequent `New()`, `NewOGImageExtractor()`, and `NewContentExtractor()` pick up the override. Defaults are `DefaultHostRPS=2.0`, `DefaultHostBurst=5`.
- `ogimage.go`: Extracts og:image/twitter:image from article HTML `<head>` (100KB limit, byte-based regex matching). `isAcceptableOGImage` rejects control-character URLs, non-`http(s)` schemes, empty hosts, and IP literals in forbidden ranges before the URL is stored. The fetch itself runs through `SafeTransport` so SSRF protection applies at the dial layer too.
- `content.go`: Uses `go-shiori/go-readability` for article text extraction (5MB response limit). Fetch goes through `SafeTransport`.
- Media extraction: Parses audio/video enclosures and iTunes duration from RSS feeds. `sanitizeMIMEType` enforces a tight pattern so a feed-supplied enclosure type can't smuggle CRLF/header bytes; `parseDuration` uses `parseSafeInt` with overflow guard and caps at 24 hours.

### Database Client (`internal/database/supabase.go`)
- Direct HTTP calls to Supabase REST API (uses shared HTTP transport)
- Retry with exponential backoff on 429/502/503/504 (up to 3 retries)
- Batch article inserts: POST arrays of 50 with `on_conflict=url_hash` + `ignore-duplicates`
- Batch image updates: `batch_update_article_images` RPC for og:image updates on duplicates
- Batch source state: `BatchUpdateSourceFetchState()` calls `batch_update_source_fetch_state` RPC (migration 020) to persist per-source etag, last_modified, consecutive_failures, and circuit_open_until in one round-trip after every fetch cycle.
- Adaptive fetch + circuit breaker: `GetActiveSources()` filters by `fetch_interval_hours`, `last_fetched_at`, and `or=(circuit_open_until.is.null,circuit_open_until.lt.{now})` so sources in an open-circuit cool-off are skipped.
- Key methods: `GetActiveSources()`, `InsertArticles()`, `BatchUpdateArticleImages()`, `BatchUpdateArticleContent()`, `BatchUpdateSourceFetchState()`, `CleanupOldArticles()`, `GetArticlesNeedingOGImage(limit, maxAttempts, cooldownHours)`, `GetArticlesNeedingContent(limit, maxAttempts, cooldownHours)`, `BumpBackfillAttempts(urlHashes, kind)`

### Data Models (`internal/models/models.go`)
- `Source` struct with `FetchIntervalHours`, `EmbeddedCategory`, `ShouldFetch()` method. Also carries conditional-GET validators (`ETag`, `LastModified`) and circuit-breaker state (`ConsecutiveFailures`, `CircuitOpenUntil`) from migration 019.
- `Article` struct with media fields and denormalized `SourceName`, `SourceSlug`, `CategoryName`, `CategorySlug`
- `NewArticle()` accepts language parameter — articles inherit language from their source
- `HashURL()` function for SHA256-based URL deduplication
- `FetchResult` for concurrent processing results — includes `ETag`, `LastModified`, and `NotModified` from the feed response so `runFetch` can persist per-source state.

### Edge Functions (`supabase/functions/`)
Caching proxy layer for iOS app with Cache-Control headers:

| Endpoint | Cache | Description |
|----------|-------|-------------|
| `/api-categories` | 24h public | Static category list |
| `/api-sources` | 1h public | RSS source list |
| `/api-articles` | 5min + ETag | Article feed with 304 support |
| `/api-search` | 1min private | Full-text search via RPC |
| `/api-health` | no-store | Liveness probe — returns `{"status":"ok"}` only (no clock/version fingerprint) |
| `/api-source-health` | 60s public | Per-source fetch health + aggregate summary (circuit/stale/high-failure counts) + `database` block (size_bytes/size_pretty/quota_pct via `get_db_size_bytes` RPC, default cap 500 MB via `SUPABASE_DB_QUOTA_BYTES`); watchdog workflow polls this |

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
supabase functions deploy api-health
supabase functions deploy api-source-health
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

Source fetch state (migration 019):
- `etag`, `last_modified`: conditional-GET validators captured on success and sent on the next fetch so the origin can reply 304 Not Modified.
- `consecutive_failures`, `circuit_open_until`: circuit breaker. After `CIRCUIT_FAILURE_THRESHOLD` consecutive fetch errors, `circuit_open_until` is set to a cool-off timestamp and `GetActiveSources()` skips the source until it elapses. Backoff doubles per additional failure, capped at `CIRCUIT_MAX_BACKOFF_HOURS`.

Key functions (after migration 027, every SECURITY DEFINER function uses `SET search_path = ''` with fully qualified references plus an in-function `CURRENT_USER` check so a future REVOKE typo or signature overload can't accidentally expose write paths):
- `cleanup_old_articles(days_to_keep)` - Service-role only. Batched 5,000-row deletes, `statement_timeout = '5min'`.
- `search_articles(search_query, result_limit)` - Returns an explicit projection (no `SETOF articles`, so backfill columns / `url_hash` / `search_vector` don't leak). Rejects empty / whitespace / >200-char queries. `SECURITY DEFINER` so it bypasses anon column grants. `statement_timeout = '3s'`. Granted to anon/authenticated.
- `batch_update_article_images(updates)` - Service-role only.
- `batch_update_article_content(updates)` - Migration 026, service-role only.
- `bump_backfill_attempts(url_hashes, kind)` - Service-role only. Rejects arrays > 10K entries. `kind` is `"image"` or `"content"`.
- `batch_update_source_fetch_state(updates)` - Service-role only. One round-trip per fetch cycle; JSONB array of per-source state (etag, last_modified, consecutive_failures, circuit_open_until, last_fetched_at).
- `get_db_size_bytes()` - Returns `pg_database_size(current_database())`. Service-role only after migration 027 — `api-source-health` Edge Function calls it with the service-role key internally.

Views:
- `articles_with_source` - Explicit projection of `articles` (migration 027 drops `url_hash` and backfill state). `security_invoker=on` so RLS + column-level grants on `articles` are honored. iOS reads through this view; column-level GRANT on `articles` exposes only the safe set plus `search_vector` (needed for `.textSearch(...)` from iOS).
- `source_health` - Per-source health snapshot (circuit_open, consecutive_failures, most_recent_article_at, articles_last_24h). `security_invoker=on`. **Revoked from anon/authenticated after migration 027** — `api-source-health` Edge Function authenticates upstream as service-role.

RLS + grants (post-027):
- `articles`: column-level `GRANT SELECT (safe-cols + search_vector)` to anon/authenticated. Backfill state + `url_hash` are service-role only.
- `fetch_logs`: `REVOKE ALL FROM anon, authenticated` (defence-in-depth on top of RLS).
- `categories`, `sources`: unchanged anon SELECT.

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
- `CIRCUIT_FAILURE_THRESHOLD` - Optional: consecutive fetch failures before the circuit trips (default `5`)
- `CIRCUIT_BASE_BACKOFF_HOURS` - Optional: initial cool-off window on trip; doubles per additional failure (default `1`)
- `CIRCUIT_MAX_BACKOFF_HOURS` - Optional: cap on the exponential circuit backoff so dead feeds still get retried daily (default `24`)
- `IMAGE_PRUNE_DAYS` - Optional: age (days) past which `image_url`/`thumbnail_url` are nulled by the daily cleanup (default `3`). Validated at startup: must be `> 0` and `<= ArticleRetentionDays`. Shared between the prune RPC and the og:image backfill candidate filter to prevent cutoff drift.
- `CONTENT_PRUNE_DAYS` - Optional: age (days) past which `articles.content` is nulled by the daily cleanup (default `2`). Same bounds as `IMAGE_PRUNE_DAYS`. Shared between the prune RPC and the content backfill candidate filter so the worker doesn't re-extract what cleanup just nulled.

Defaults in `internal/config/config.go`:
- `MaxConcurrent`: 5 sources processed simultaneously
- `ArticleRetentionDays`: 7 days (also drives fetch_logs retention via `CleanupOldFetchLogs`)

Graceful shutdown: the worker installs a `signal.NotifyContext` handler for
SIGINT/SIGTERM at startup. In-flight goroutines check `ctx.Done()` and HTTP
requests cancel via request context, so GitHub Actions cancellations and
runner rotations exit without orphaning batches.

## Testing

Unit tests cover Go packages and Deno Edge Functions. **All Go packages are held at 100% statement coverage**; `test.yml` fails the build if total coverage drops below 100.0%. Defensive branches that can't fail with real inputs (e.g. `json.Marshal` on statically-typed payloads, `crypto/rand.Read`) are made reachable via package-level function vars (`jsonMarshal`, `randRead`) that tests swap — follow that pattern when adding new similar code.

| Package | Coverage | Key Tests |
|---------|----------|-----------|
| `internal/models` | 100% | HashURL, NewArticle, ShouldFetch, CategoryName |
| `internal/config` | 100% | Load + env var validation (including HOST_RATE_LIMIT_*, BACKFILL_*, and CIRCUIT_*) |
| `internal/httputil` | 100% | SharedTransport, NewClient, NewClientWithRedirectLimit, RateLimitingTransport (per-host serialization, cross-host independence, ctx-cancel short-circuit, nil-base default, zero-maxRedirects path) |
| `internal/parser` | 100% | cleanHTML (including partial-tag and no-closing-tag edges), extractImageURL, OG image (body-read errors), content extraction (readability errors), itemToArticle (embedded category), ParseFeed (200/304/non-2xx + conditional-GET headers, bad-URL + transport errors), parseDuration (too-many-parts default) |
| `internal/database` | 100% | Batch inserts, batch image RPC, BatchUpdateSourceFetchState, GetActiveSources (circuit filter), retry logic, BumpBackfillAttempts, plus bad-URL/transport/marshal/decode error branches across every method |
| `internal/logger` | 100% | Level filtering, text + JSON output format, `With()` field propagation, nil-atomic fallbacks, toSlogLevel default, subprocess-driven Fatalf |
| `main` | 100% | processSource (+ panic recovery), runFetch, nextCircuitOpenUntil, buildSourceFetchState, runBackfill, newRunID fallback, plus subprocess-driven TestMain that exercises every main() command (fetch/cleanup/backfill-images/backfill-content + config-load and runtime-error paths) |
| `_shared/*.ts` | — | cache, cors, etag utilities |

Run tests:
```bash
make test           # All tests
make test-go-cover  # Go with coverage report
make test-deno      # Deno Edge Function tests
```

## GitHub Actions

- **fetch-rss.yml**: Every 2 hours + manual trigger. Runs the Go RSS worker against the Supabase production DB.
- **cleanup.yml**: Daily at 3 AM UTC + manual trigger. Removes articles older than `ArticleRetentionDays`.
- **backfill.yml**: Daily at 04:30 UTC + manual trigger. Two parallel jobs (`backfill-images`, `backfill-content`) that drain articles missing og:image/content. The `workflow_dispatch` form takes a `kind` input (`both`/`images`/`content`) so you can run one in isolation. Cooldowns and attempt caps live in the DB queries (`BACKFILL_COOLDOWN_HOURS`, `BACKFILL_MAX_ATTEMPTS`); the daily cadence matches the 24h cooldown so re-runs are cheap.
- **test.yml**: Runs on push/PR to master (Go tests with race detector + coverage, 100% coverage gate, golangci-lint, govulncheck, Deno tests). The coverage step parses `go tool cover -func` output and fails the job if total coverage is below 100.0%, listing sub-100% functions.
- **security.yml**: Runs on push/PR to master + weekly (Mon 06:00 UTC). Jobs: secret scan (gitleaks + TruffleHog), Go SAST (gosec), govulncheck, Trivy filesystem scan (vuln/secret/misconfig), CycloneDX SBOM artifact.
- **pr-checks.yml**: Runs on PR to master only. Jobs: PR title conventional-commits (`feat|fix|chore|…` prefix), go.mod Sync (fails if `go mod tidy` produces a diff), Migration Format (enforces `NNN_*.sql`, no gaps, no duplicate prefixes).
- **deploy-functions.yml**: Builds + deploys Edge Functions on push to master under `supabase/functions/**`. Gated by the `production` GitHub Environment — pauses for required-reviewer approval in the Actions UI before shipping. `SUPABASE_ACCESS_TOKEN` + `SUPABASE_PROJECT_REF` live on that Environment, not at repo scope, so they aren't exposed to other workflows.
- **watchdog.yml**: Every 6 hours (`:15` past the hour) + manual trigger. Calls `api-source-health` and fails the job (→ GitHub email) when `circuit_open_count`, `stale_count`, `high_failure_count`, or `database.quota_pct` (trips at 60% — tightened from 70% after the May 2026 cleanup brought the DB to 21%) exceed thresholds set inline in the workflow. The DB quota check tolerates `database == null` (RPC failure) without alerting so transient size-fetch errors don't false-page.
- **lgpd-conformance.yml** + **gdpr-conformance.yml**: Push/PR to master + weekly Mon 07:00 UTC. Four parallel jobs each (`pii-scan`, `docs-presence`, `operational-controls`, `structural-integrity`). Enforce regulator-specific PII bans (CPF/CNPJ for LGPD; IBAN + EU/EEA phone for GDPR; SSN for CCPA in both), a case-insensitive email allowlist via `.github/pii-allowlist.txt`, no `RemoteAddr` / `X-Forwarded-For` / `X-Real-IP` / `CF-Connecting-IP` in `rss-worker/`, presence + non-emptiness of every doc under `docs/{privacy,lgpd-conformance,gdpr-conformance,ccpa-conformance,ropa,data-retention}.md`, `ArticleRetentionDays = 7`, no plaintext `http://` in migrations, the literal `No PII redaction layer required` in both regulator docs, RLS still on, schema-qualifier-aware CREATE TABLE allowlist, ALTER TABLE ADD COLUMN PII bans, and `| GitHub ` / `| Supabase ` rows in `docs/ropa.md`. Run with a `cancel-in-progress` concurrency block for PRs.

All 19 job names from `test.yml`, `security.yml`, `pr-checks.yml`, `lgpd-conformance.yml`, and `gdpr-conformance.yml` are required status checks on `master` via branch protection. Direct pushes to `master` are blocked (even for admins); every change goes through a PR with `delete_branch_on_merge` + squash-only merges.

## Data Protection Conformance

The backend asserts and enforces a no-end-user-PII posture: it processes public RSS news only, no personal data of identified or identifiable natural persons. The `author` byline on articles is treated under the journalism exemption (GDPR Art. 85 / LGPD Art. 4 § II / CCPA §1798.145(k)).

Position docs (`docs/privacy.md`, `docs/lgpd-conformance.md`, `docs/gdpr-conformance.md`, `docs/ccpa-conformance.md`, `docs/ropa.md`, `docs/data-retention.md`) describe the posture; the conformance workflows above keep it true over time. CCPA's only distinctive identifier (US SSN) is enforced by an extra step in each existing pii-scan job rather than a third parallel workflow.

Adding personal-data processing requires updating: (1) the relevant conformance doc, (2) the `pii-scan` exclusion or allowlist if the new data is intentional, (3) `docs/ropa.md` if a new subprocessor is involved, and (4) any allowlist (`structural-integrity` table allowlist, PII-column regex, email allowlist).

Secrets:
- **Repo scope** — `SUPABASE_URL`, `SUPABASE_SERVICE_ROLE_KEY` (used by `fetch-rss.yml`, `cleanup.yml`, `backfill.yml`; the watchdog only needs `SUPABASE_URL`).
- **`production` Environment** — `SUPABASE_ACCESS_TOKEN`, `SUPABASE_PROJECT_REF` (used by `deploy-functions.yml` only; gated by required-reviewer approval).

## Monitoring

Check `fetch_logs` table in Supabase Table Editor for:
- `status`: running / completed / partial_failure / failed
- `articles_inserted`, `articles_skipped`, `errors`

GitHub Actions logs: Repository → Actions tab
