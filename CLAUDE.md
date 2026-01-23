# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Pulse Backend is a self-hosted news aggregation backend for the Pulse iOS app. It uses Go for RSS fetching and Supabase (PostgreSQL) for database and auto-generated REST API.

**Tech Stack:** Go 1.23 | Supabase | GitHub Actions | PostgreSQL

## Architecture

```
GitHub Actions (every 15 min)
    ↓
Go RSS Worker (rss-worker/)
    ├─ Fetch RSS feeds (14 sources)
    ├─ Parse with gofeed library
    ├─ Enrich: og:image extraction (5 workers)
    ├─ Enrich: content extraction (3 workers)
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

```bash
cd rss-worker

# Build
go mod tidy && go build -o rss-worker .

# Run locally (requires env vars)
export SUPABASE_URL="https://your-project.supabase.co"
export SUPABASE_SERVICE_ROLE_KEY="your-service-role-key"
go run .

# Special commands
go run . cleanup           # Remove articles older than 30 days
go run . backfill-images   # Fetch og:images for articles missing images (500 per run)
go run . backfill-content  # Extract full content for articles (200 per run)
```

## Project Structure

```
pulse-backend/
├── rss-worker/                        # Main Go application
│   ├── main.go                        # Entry point with command routing
│   └── internal/
│       ├── config/config.go           # Env config (SUPABASE_URL, SUPABASE_SERVICE_ROLE_KEY)
│       ├── models/models.go           # Data models (Article, Source, Category, FetchLog)
│       ├── parser/
│       │   ├── parser.go              # RSS parsing with gofeed + enrichment orchestration
│       │   ├── ogimage.go             # og:image meta tag extraction
│       │   └── content.go             # Full article content extraction (go-readability)
│       └── database/supabase.go       # Supabase REST API client
├── supabase/
│   ├── migrations/
│   │   └── 001_initial_schema.sql     # Database schema (run in Supabase SQL Editor)
│   └── functions/                     # Edge Functions (caching proxy)
│       ├── _shared/                   # Shared utilities
│       │   ├── cors.ts                # CORS headers
│       │   ├── cache.ts               # Cache-Control utilities
│       │   ├── etag.ts                # ETag generation
│       │   └── supabase-proxy.ts      # Proxy logic
│       ├── api-categories/index.ts    # Categories endpoint (24h cache)
│       ├── api-sources/index.ts       # Sources endpoint (1h cache)
│       ├── api-articles/index.ts      # Articles endpoint (5min + ETag)
│       └── api-search/index.ts        # Search endpoint (1min private)
├── .github/workflows/
│   ├── fetch-rss.yml                  # Runs every 15 minutes
│   └── cleanup.yml                    # Runs daily at 3 AM UTC
└── docs/ios-integration.md            # iOS app integration guide
```

## Key Components

### Main Entry Point (`main.go`)
- Command routing: default fetch, `cleanup`, `backfill-images`, `backfill-content`
- Concurrent source processing with semaphore (default: 5 concurrent)
- Fetch logging to `fetch_logs` table

### Parser Module (`internal/parser/`)
- `parser.go`: Orchestrates RSS parsing via `mmcdole/gofeed`, then enriches articles with og:images (5 workers) and content (3 workers)
- `ogimage.go`: Extracts og:image/twitter:image from article HTML `<head>` (100KB limit)
- `content.go`: Uses `go-shiori/go-readability` for article text extraction

### Database Client (`internal/database/supabase.go`)
- Direct HTTP calls to Supabase REST API
- Deduplication via `url_hash` (SHA256 of URL) - returns 409 on conflict, updates image_url if better
- Key methods: `GetActiveSources()`, `InsertArticles()`, `CleanupOldArticles()`, backfill queries

### Data Models (`internal/models/models.go`)
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

Tables: `categories` (8), `sources` (14 pre-configured), `articles` (with full-text search via tsvector), `fetch_logs`

Key functions:
- `cleanup_old_articles(days_to_keep)` - Called by cleanup command
- `search_articles(search_query, result_limit)` - Full-text search

View: `articles_with_source` - Joins articles with source and category info

## Configuration

Environment variables:
- `SUPABASE_URL` - Required
- `SUPABASE_SERVICE_ROLE_KEY` - Required (keep secret, needed for writes)

Defaults in `internal/config/config.go`:
- `MaxConcurrent`: 5 sources processed simultaneously
- `ArticleRetentionDays`: 30 days

## GitHub Actions

- **fetch-rss.yml**: Every 15 minutes + manual trigger
- **cleanup.yml**: Daily at 3 AM UTC + manual trigger

Secrets needed: `SUPABASE_URL`, `SUPABASE_SERVICE_ROLE_KEY`

## Monitoring

Check `fetch_logs` table in Supabase Table Editor for:
- `status`: running / completed / failed
- `articles_inserted`, `articles_skipped`, `errors`

GitHub Actions logs: Repository → Actions tab
