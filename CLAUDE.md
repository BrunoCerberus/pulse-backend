# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Pulse Backend is a self-hosted news aggregation backend for the Pulse iOS app. It uses Go for RSS fetching and Supabase (PostgreSQL) for database and auto-generated REST API.

**Tech Stack:** Go 1.23 | Supabase | GitHub Actions | PostgreSQL

## Architecture

```
GitHub Actions (every 2 hours)
    ↓
Go RSS Worker (rss-worker/)
    ├─ Fetch RSS feeds (48 sources: articles, podcasts, videos)
    ├─ Parse with gofeed library
    ├─ Enrich: og:image extraction (5 workers)
    ├─ Enrich: content extraction (3 workers)
    ├─ Extract: media enclosures (audio/video URLs, duration)
    └─ Insert to Supabase (dedup via url_hash)
        ↓
PostgreSQL (articles, sources, categories, fetch_logs)
        ↓
Edge Functions (caching proxy with Cache-Control headers)
    ├── /api-categories  → Cache: 24h
    ├── /api-sources     → Cache: 1h
    ├── /api-articles    → Cache: 5min + ETag
    └── /api-search      → Cache: 1min (private)
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
│       │   ├── supabase.go            # Supabase REST API client
│       │   └── supabase_test.go       # Database client tests (69% coverage)
│       └── httputil/
│           └── transport.go           # Shared HTTP transport with connection pooling
├── supabase/
│   ├── migrations/
│   │   ├── 001_initial_schema.sql     # Core database schema
│   │   ├── 002_add_media_support.sql  # Podcast/video media fields
│   │   ├── 003_add_podcast_video_sources.sql  # 34 curated podcast/video sources
│   │   ├── 004_update_articles_with_source_view.sql  # Expose media fields in API view
│   │   ├── 005_fix_security_issues.sql  # Harden RLS, view, function security
│   │   ├── 006_add_composite_indexes.sql  # Composite indexes for performance
│   │   └── 007_add_language_support.sql   # Language column on sources & articles
│   └── functions/                     # Edge Functions (caching proxy)
│       ├── _shared/                   # Shared utilities
│       │   ├── cors.ts                # CORS headers
│       │   ├── cors_test.ts           # CORS tests
│       │   ├── cache.ts               # Cache-Control utilities
│       │   ├── cache_test.ts          # Cache tests
│       │   ├── etag.ts                # ETag generation
│       │   ├── etag_test.ts           # ETag tests
│       │   └── supabase-proxy.ts      # Proxy logic
│       ├── api-categories/index.ts    # Categories endpoint (24h cache)
│       ├── api-sources/index.ts       # Sources endpoint (1h cache)
│       ├── api-articles/index.ts      # Articles endpoint (5min + ETag)
│       └── api-search/index.ts        # Search endpoint (1min private)
├── .github/workflows/
│   ├── fetch-rss.yml                  # Runs every 2 hours
│   ├── cleanup.yml                    # Runs daily at 3 AM UTC
│   └── test.yml                       # Unit tests on push/PR
└── docs/ios-integration.md            # iOS app integration guide
```

## Key Components

### Main Entry Point (`main.go`)
- Command routing: default fetch, `cleanup`, `backfill-images`, `backfill-content`
- Concurrent source processing with semaphore (default: 5 concurrent)
- Fetch logging to `fetch_logs` table

### HTTP Utilities (`internal/httputil/`)
- `transport.go`: Shared `http.Transport` with tuned connection pooling (`MaxIdleConnsPerHost: 10`, HTTP/2 enabled). All HTTP clients use this shared transport via `httputil.NewClient(timeout)` or `httputil.NewClientWithRedirectLimit(timeout, maxRedirects)` to enable connection reuse across workers.

### Parser Module (`internal/parser/`)
- `parser.go`: Orchestrates RSS parsing via `mmcdole/gofeed`, then enriches articles with og:images (5 workers) and content (3 workers). Also extracts media enclosures (audio/video) for podcasts and videos.
- `ogimage.go`: Extracts og:image/twitter:image from article HTML `<head>` (100KB limit, byte-based regex matching)
- `content.go`: Uses `go-shiori/go-readability` for article text extraction (5MB response limit)
- Media extraction: Parses audio/video enclosures and iTunes duration from RSS feeds

### Database Client (`internal/database/supabase.go`)
- Direct HTTP calls to Supabase REST API (uses shared HTTP transport)
- Deduplication via `url_hash` (SHA256 of URL) - returns 409 on conflict, updates image_url if better
- Key methods: `GetActiveSources()`, `InsertArticles()`, `CleanupOldArticles()`, backfill queries

### Data Models (`internal/models/models.go`)
- `Source` and `Article` structs with `Language` field (ISO 639-1, e.g. `"en"`, `"pt"`)
- `Article` struct with media fields: `MediaType`, `MediaURL`, `MediaDuration`, `MediaMIMEType`
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

Tables: `categories` (10, including Podcasts & Videos), `sources` (48 pre-configured), `articles` (with full-text search via tsvector and media fields), `fetch_logs`

Language support:
- `sources.language`: ISO 639-1 code (VARCHAR(5), default `'en'`), e.g. `'en'`, `'pt'`, `'es'`
- `articles.language`: Inherited from source at insert time, indexed for filtering

Article media fields (for podcasts/videos):
- `media_type`: 'podcast' or 'video'
- `media_url`: Direct URL to audio/video file
- `media_duration`: Duration in seconds
- `media_mime_type`: MIME type (audio/mpeg, video/mp4, etc.)

Key functions:
- `cleanup_old_articles(days_to_keep)` - Called by cleanup command
- `search_articles(search_query, result_limit)` - Full-text search

View: `articles_with_source` - Joins articles with source, category, media info, and language

## Configuration

Environment variables:
- `SUPABASE_URL` - Required
- `SUPABASE_SERVICE_ROLE_KEY` - Required (keep secret, needed for writes)

Defaults in `internal/config/config.go`:
- `MaxConcurrent`: 5 sources processed simultaneously
- `ArticleRetentionDays`: 30 days

## Testing

Unit tests cover Go packages and Deno Edge Functions:

| Package | Coverage | Key Tests |
|---------|----------|-----------|
| `internal/models` | 100% | HashURL, NewArticle |
| `internal/config` | 100% | Load with env var validation |
| `internal/httputil` | 100% | SharedTransport, NewClient, NewClientWithRedirectLimit |
| `internal/parser` | 92% | cleanHTML, extractImageURL, OG image, content extraction, itemToArticle |
| `internal/database` | 77% | All Supabase client methods with httptest mocking |
| `main` | 18% | processSource, processOGImageBackfill, processContentBackfill |
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
- **test.yml**: Runs on push/PR to main (Go + Deno tests)

Secrets needed: `SUPABASE_URL`, `SUPABASE_SERVICE_ROLE_KEY`

## Monitoring

Check `fetch_logs` table in Supabase Table Editor for:
- `status`: running / completed / failed
- `articles_inserted`, `articles_skipped`, `errors`

GitHub Actions logs: Repository → Actions tab
