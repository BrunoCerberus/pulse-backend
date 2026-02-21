# AGENTS.md

This file provides guidance to AI coding agents when working with this repository.

## Project Overview

Pulse Backend is a self-hosted news aggregation backend for the Pulse iOS app. It uses Go for RSS fetching and Supabase (PostgreSQL) for database and auto-generated REST API.

**Tech Stack:** Go 1.23 | Supabase | GitHub Actions | PostgreSQL | Deno (Edge Functions)

## Architecture

```
GitHub Actions (every 2 hours)
    ↓
Go RSS Worker (rss-worker/)
    ├─ Fetch RSS feeds (133 sources: articles, podcasts, videos)
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
│       │   ├── supabase.go            # Supabase REST API client
│       │   └── supabase_test.go       # Database client tests (69% coverage)
│       └── httputil/
│           └── transport.go           # Shared HTTP transport with connection pooling
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
│   │   └── 010_add_pt_es_podcasts_videos.sql  # PT & ES podcasts, videos, politics
│   └── functions/                     # Edge Functions (Deno/TypeScript)
│       ├── _shared/                   # Shared utilities
│       │   ├── cors.ts / cors_test.ts
│       │   ├── cache.ts / cache_test.ts
│       │   ├── etag.ts / etag_test.ts
│       │   └── supabase-proxy.ts
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

### Go RSS Worker (`rss-worker/`)

| Component | File | Description |
|-----------|------|-------------|
| Entry Point | `main.go` | Command routing: fetch, cleanup, backfill-images, backfill-content |
| Config | `internal/config/config.go` | Loads SUPABASE_URL, SUPABASE_SERVICE_ROLE_KEY from env |
| Models | `internal/models/models.go` | Article (with media fields), Source, Category, FetchLog structs; HashURL() for dedup |
| Parser | `internal/parser/parser.go` | RSS parsing via gofeed + parallel enrichment + media extraction |
| OG Image | `internal/parser/ogimage.go` | Extracts og:image from article HTML (100KB limit) |
| Content | `internal/parser/content.go` | Extracts article text via go-readability |
| Database | `internal/database/supabase.go` | Supabase REST API client with deduplication |
| HTTP Utils | `internal/httputil/transport.go` | Shared HTTP transport with tuned connection pooling; `NewClient(timeout)` and `NewClientWithRedirectLimit(timeout, maxRedirects)` |

### Edge Functions (`supabase/functions/`)

| Endpoint | Cache | Description |
|----------|-------|-------------|
| `/api-categories` | 24h public | Static category list |
| `/api-sources` | 1h public | RSS source list |
| `/api-articles` | 5min + ETag | Article feed with 304 support |
| `/api-search` | 1min private | Full-text search via RPC |

## Testing

Tests use Go's standard testing package with `httptest` for mocking HTTP calls, and Deno's built-in test runner for Edge Functions.

| Package | Coverage | Description |
|---------|----------|-------------|
| `internal/models` | 100% | HashURL, NewArticle |
| `internal/config` | 100% | Env var loading and validation |
| `internal/httputil` | 100% | SharedTransport, NewClient, NewClientWithRedirectLimit |
| `internal/parser` | 92% | HTML cleaning, image extraction, OG/content fetching, itemToArticle |
| `internal/database` | 77% | Supabase client methods with httptest mocking |
| `main` | 18% | processSource, processOGImageBackfill, processContentBackfill |
| `_shared/*.ts` | — | Cache, CORS, ETag utilities |

Run tests before committing:
```bash
make test
```

## Configuration

Required environment variables:
- `SUPABASE_URL` - Supabase project URL
- `SUPABASE_SERVICE_ROLE_KEY` - Service role key (keep secret, needed for writes)

Defaults in `internal/config/config.go`:
- `MaxConcurrent`: 5 sources processed simultaneously
- `ArticleRetentionDays`: 30 days

## Database Schema

Tables:
- `categories` - 10 categories (including Podcasts & Videos)
- `sources` - 133 pre-configured feeds (articles, podcasts, YouTube channels)
- `articles` - News articles with full-text search (tsvector) and media fields (media_type, media_url, media_duration, media_mime_type)
- `fetch_logs` - Monitoring records

Key functions:
- `cleanup_old_articles(days_to_keep)` - Remove old articles
- `search_articles(search_query, result_limit)` - Full-text search

View:
- `articles_with_source` - Joins articles with source, category, and media info

## Code Style Guidelines

- Go code follows standard Go conventions (`go fmt`, `go vet`)
- Use table-driven tests for comprehensive coverage
- HTTP calls should be mocked with `httptest.Server` in tests
- New HTTP clients must use `httputil.NewClient(timeout)` or `httputil.NewClientWithRedirectLimit(timeout, maxRedirects)` to share the connection pool
- Edge Functions use TypeScript with Deno
- All new code should include tests

## GitHub Actions

| Workflow | Schedule | Description |
|----------|----------|-------------|
| `fetch-rss.yml` | Every 2 hours | Fetch RSS feeds |
| `cleanup.yml` | Daily 3 AM UTC | Remove old articles |
| `test.yml` | On push/PR | Run unit tests |

Secrets needed: `SUPABASE_URL`, `SUPABASE_SERVICE_ROLE_KEY`
