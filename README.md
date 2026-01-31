# Pulse Backend

Self-hosted news aggregation backend for the Pulse iOS app. Uses **Go** for RSS fetching and **Supabase** for database and API.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          GitHub Actions (Free)                               │
│                      Scheduled: Every 2 hours                                │
│                                                                              │
│    ┌─────────────────────────────────────────────────────────────────────┐  │
│    │                         Go RSS Worker                                │  │
│    │                                                                      │  │
│    │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐            │  │
│    │  │ Guardian │  │   BBC    │  │ Podcasts │  │ YouTube  │  ...      │  │
│    │  │   RSS    │  │   RSS    │  │   RSS    │  │   RSS    │            │  │
│    │  └────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘            │  │
│    │       └─────────────┴──────┬──────┴─────────────┘                   │  │
│    │                            ▼                                        │  │
│    │                   Parse → Deduplicate → Insert                      │  │
│    └────────────────────────────┼────────────────────────────────────────┘  │
└─────────────────────────────────┼───────────────────────────────────────────┘
                                  │
                                  ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Supabase (Free Tier)                                 │
│                                                                              │
│   ┌─────────────┐    ┌─────────────┐    ┌─────────────────────────────┐    │
│   │  articles   │    │   sources   │    │       Edge Functions        │    │
│   │  (30 days)  │    │ (48 feeds)  │    │    (Caching Proxy Layer)    │    │
│   └─────────────┘    └─────────────┘    └─────────────────────────────┘    │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
                                  │
                                  ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                            Pulse iOS App                                     │
│                                                                              │
│   HTTP calls to Edge Functions with Cache-Control support                   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Free Tier Limits

| Service | Limit | Our Usage | Status |
|---------|-------|-----------|--------|
| Supabase DB | 500 MB | ~50 MB | ✅ |
| Supabase API | 500K req/mo | ~100K | ✅ |
| Supabase Edge Functions | 500K invocations/mo | Varies | ✅ |
| GitHub Actions | 2,000 min/mo | ~720 min | ✅ |

## Quick Start

### 1. Create Supabase Project

1. Go to [supabase.com](https://supabase.com) and create a free account
2. Create a new project (remember your database password)
3. Wait for the project to initialize (~2 minutes)

### 2. Run Database Migrations

1. In Supabase Dashboard, go to **SQL Editor**
2. Run migrations in order:
   - `supabase/migrations/001_initial_schema.sql` - Core schema
   - `supabase/migrations/002_add_media_support.sql` - Podcast/video support
   - `supabase/migrations/003_add_podcast_video_sources.sql` - Curated sources
   - `supabase/migrations/004_update_articles_with_source_view.sql` - Expose media fields in API

This creates:
- `categories` table with 10 categories (including Podcasts & Videos)
- `sources` table with 48 feeds (articles, podcasts, YouTube channels)
- `articles` table with full-text search and media fields
- `fetch_logs` table for monitoring
- Row Level Security policies
- Helper functions

### 3. Get Your API Keys

In Supabase Dashboard → **Settings** → **API**:

| Key | Purpose | Where to Use |
|-----|---------|--------------|
| **Project URL** | API base URL | iOS app + Go worker |
| **anon (public)** | Read-only access | iOS app |
| **service_role** | Full access | Go worker only (keep secret!) |

### 4. Set Up GitHub Repository

```bash
cd ~/pulse-backend
git init
git add .
git commit -m "Initial commit: Pulse backend"

# Create repo on GitHub, then:
git remote add origin git@github.com:YOUR_USERNAME/pulse-backend.git
git push -u origin main
```

### 5. Configure GitHub Secrets

In your GitHub repo → **Settings** → **Secrets and variables** → **Actions**:

| Secret Name | Value |
|-------------|-------|
| `SUPABASE_URL` | Your Supabase Project URL |
| `SUPABASE_SERVICE_ROLE_KEY` | Your service_role key |

### 6. Enable GitHub Actions

The workflows are already configured:
- `fetch-rss.yml` - Runs every 2 hours
- `cleanup.yml` - Runs daily at 3 AM UTC

Go to **Actions** tab and enable workflows if prompted.

### 7. Deploy Edge Functions

```bash
# Install Supabase CLI
brew install supabase/tap/supabase

# Login and link project
supabase login
supabase link --project-ref YOUR_PROJECT_REF

# Deploy all functions
supabase functions deploy
```

The Edge Functions provide a caching layer for the iOS app with Cache-Control headers.

### 8. Test the Worker

Trigger a manual run:
1. Go to **Actions** → **Fetch RSS Feeds**
2. Click **Run workflow**
3. Check the logs to see articles being fetched

### 9. Verify in Supabase

Go to **Table Editor** → **articles** to see the fetched articles.

## Project Structure

```
pulse-backend/
├── README.md                          # This file
├── Makefile                           # Common commands (make help)
├── docs/
│   └── ios-integration.md             # iOS app integration guide
├── supabase/
│   ├── config.toml                    # Edge Functions config
│   ├── migrations/
│   │   ├── 001_initial_schema.sql     # Core database schema
│   │   ├── 002_add_media_support.sql  # Podcast/video columns
│   │   ├── 003_add_podcast_video_sources.sql  # Curated sources
│   │   └── 004_update_articles_with_source_view.sql  # Expose media in API
│   └── functions/                     # Edge Functions (caching proxy)
│       ├── _shared/                   # Shared utilities + tests
│       ├── api-categories/            # Categories endpoint (24h cache)
│       ├── api-sources/               # Sources endpoint (1h cache)
│       ├── api-articles/              # Articles endpoint (5min + ETag)
│       └── api-search/                # Search endpoint (1min private)
├── rss-worker/
│   ├── go.mod                         # Go module definition
│   ├── main.go                        # Entry point
│   └── internal/
│       ├── config/                    # Configuration + tests
│       ├── models/                    # Data models + tests
│       ├── parser/                    # RSS parsing + tests
│       └── database/                  # Supabase client + tests
└── .github/
    └── workflows/
        ├── fetch-rss.yml              # RSS fetch job (every 2 hours)
        ├── cleanup.yml                # Cleanup job (daily)
        └── test.yml                   # Unit tests (on push/PR)
```

## Content Sources

Pre-configured sources (edit in Supabase Dashboard → **sources** table):

### News Articles (14 sources)
| Source | Category |
|--------|----------|
| The Guardian (World, Tech, Business, Sport, Science) | Various |
| BBC News (World, Tech, Business, Health) | Various |
| NPR News | World |
| Ars Technica, TechCrunch, The Verge | Technology |
| Science Daily | Science |

### Podcasts (17 sources)
| Source | Topic |
|--------|-------|
| The Vergecast, ATP, Darknet Diaries | Technology |
| The Daily, Up First, Pod Save America | News & Politics |
| Radiolab, StarTalk, Science Vs | Science |
| Huberman Lab, Peter Attia, On Purpose | Health |
| Bill Simmons, Pardon My Take, Ringer NBA | Sports |
| How I Built This, Acquired, All-In | Business |
| SmartLess, Conan O'Brien, Armchair Expert | Entertainment |

### YouTube Channels (17 sources)
| Source | Topic |
|--------|-------|
| MKBHD, Fireship, Linus Tech Tips | Technology |
| Veritasium, Kurzgesagt, SmarterEveryDay | Science |
| Vox, PBS NewsHour | News |
| Doctor Mike, Jeff Nippard | Health |
| JomBoy Media, Secret Base | Sports |
| CNBC, Bloomberg | Business |
| First We Feast, Tonight Show, Hot Ones | Entertainment |

### Adding New Sources

1. Go to Supabase Dashboard → **Table Editor** → **sources**
2. Click **Insert row**
3. Fill in:
   - `name`: Display name
   - `slug`: Unique identifier (lowercase, hyphens)
   - `feed_url`: RSS feed URL
   - `category_id`: Select from categories table
   - `is_active`: true

## API Endpoints

### Edge Functions (Recommended for iOS)

Edge Functions provide caching for better performance:

```bash
# Base URL: https://your-project.supabase.co/functions/v1

# Get categories (24h cache)
GET /api-categories

# Get sources (1h cache)
GET /api-sources

# Get latest articles (5min cache + ETag for 304)
GET /api-articles?order=published_at.desc&limit=20

# Get articles by category
GET /api-articles?category_slug=eq.technology&order=published_at.desc&limit=20

# Get podcasts
GET /api-articles?category_slug=eq.podcasts&order=published_at.desc&limit=20

# Get videos
GET /api-articles?category_slug=eq.videos&order=published_at.desc&limit=20

# Filter by media type
GET /api-articles?media_type=eq.podcast&limit=20
GET /api-articles?media_type=eq.video&limit=20

# Search articles (1min private cache)
GET /api-search?q=climate&limit=20
```

No authentication required - endpoints are public read-only.

### Direct REST API (Alternative)

Supabase auto-generates REST endpoints (requires `apikey` header):

```bash
# Base URL: https://your-project.supabase.co/rest/v1

GET /articles_with_source?order=published_at.desc&limit=20
GET /categories?order=display_order
GET /sources?is_active=eq.true
GET /rpc/search_articles?search_query=climate&result_limit=20
```

## iOS App Integration

See [docs/ios-integration.md](docs/ios-integration.md) for detailed instructions on updating the Pulse iOS app.

**Quick summary:**
1. Add Supabase Swift SDK
2. Create `SupabaseNewsService` implementing `NewsService`
3. Swap service registration in `PulseSceneDelegate`

## Local Development

```bash
# Set environment variables
export SUPABASE_URL="https://your-project.supabase.co"
export SUPABASE_SERVICE_ROLE_KEY="your-service-role-key"

# Run the worker
make run

# Run cleanup
make cleanup

# Or without Make:
cd rss-worker && go run .
cd rss-worker && go run . cleanup
```

## Make Commands

Run `make help` to see all available commands:

```bash
# Testing
make test              # Run all tests (Go + Deno)
make test-go           # Run Go tests
make test-go-cover     # Run Go tests with coverage
make test-go-race      # Run Go tests with race detector
make test-deno         # Run Deno Edge Function tests

# Build & Run
make build             # Build the RSS worker binary
make run               # Run the RSS worker (fetch feeds)
make cleanup           # Remove articles older than 30 days
make backfill-images   # Fetch og:images for articles missing images
make backfill-content  # Extract content for articles

# Supabase Functions
make deploy            # Deploy all Edge Functions
make deploy-categories # Deploy api-categories
make deploy-sources    # Deploy api-sources
make deploy-articles   # Deploy api-articles
make deploy-search     # Deploy api-search
make functions-serve   # Run Edge Functions locally

# Utilities
make clean             # Remove build artifacts
```

## Monitoring

### Fetch Logs

Check the `fetch_logs` table in Supabase:
- `status`: running / completed / failed
- `articles_inserted`: New articles added
- `articles_skipped`: Duplicates skipped
- `errors`: Any errors encountered

### GitHub Actions

View workflow runs at:
`https://github.com/YOUR_USERNAME/pulse-backend/actions`

## Troubleshooting

### No articles appearing

1. Check GitHub Actions logs for errors
2. Verify secrets are set correctly
3. Check `fetch_logs` table for error messages
4. Ensure RSS feed URLs are accessible

### Duplicate articles

The system uses URL hashing for deduplication. If you see duplicates:
- Check if the source provides different URLs for the same article
- The `url_hash` column enforces uniqueness

### Database approaching limit

1. Reduce `ArticleRetentionDays` in config (default: 30)
2. Manually run cleanup: `go run . cleanup`
3. Reduce number of active sources

## Cost Breakdown

**Monthly cost: $0** (within free tiers)

| Service | Monthly Cost |
|---------|--------------|
| Supabase | $0 (free tier) |
| GitHub Actions | $0 (free for public repos, 2000 min for private) |
| **Total** | **$0** |

## Scaling Beyond Free Tier

If you outgrow the free tier:

| Upgrade | Cost | Benefit |
|---------|------|---------|
| Supabase Pro | $25/mo | 8GB DB, 250GB bandwidth |
| GitHub Actions | $4/1000 min | More workflow minutes |

## License

MIT
