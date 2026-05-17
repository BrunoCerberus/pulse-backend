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
│   │  (7 days)   │    │ (133 feeds) │    │    (Caching Proxy Layer)    │    │
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
| Supabase API | 500K req/mo | ~200K | ✅ |
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
   - `supabase/migrations/005_fix_security_issues.sql` - Harden RLS, view, and function security
   - `supabase/migrations/006_add_composite_indexes.sql` - Composite indexes for query performance
   - `supabase/migrations/007_add_language_support.sql` - Language column on sources & articles
   - `supabase/migrations/008_add_pt_es_sources.sql` - Portuguese & Spanish RSS sources
   - `supabase/migrations/009_add_more_pt_es_sources.sql` - More PT & ES sources
   - `supabase/migrations/010_add_pt_es_podcasts_videos.sql` - PT & ES podcasts, videos, politics
   - `supabase/migrations/011_revoke_cleanup_from_anon.sql` - Restrict cleanup function to service role only
   - `supabase/migrations/012_add_content_to_search_vector.sql` - Include content in full-text search
   - `supabase/migrations/013_drop_fetch_interval_minutes.sql` - Remove unused column
   - `supabase/migrations/014_add_batch_image_update_rpc.sql` - Batch image update RPC
   - `supabase/migrations/015_add_fetch_interval_hours.sql` - Adaptive fetch frequency
   - `supabase/migrations/016_denormalize_articles.sql` - Denormalize source/category into articles
   - `supabase/migrations/017_backfill_denormalized_articles.sql` - Backfill denormalized columns
   - `supabase/migrations/018_add_backfill_tracking.sql` - Attempt counters + cooldown RPC for image/content backfills
   - `supabase/migrations/019_add_source_fetch_state_columns.sql` - `etag`, `last_modified`, `consecutive_failures`, `circuit_open_until` on sources
   - `supabase/migrations/020_add_source_health_infra.sql` - `batch_update_source_fetch_state` RPC + `source_health` view
   - `supabase/migrations/021_batch_cleanup_old_articles.sql` - Batch `cleanup_old_articles` + per-function `statement_timeout` to avoid 57014 timeouts
   - `supabase/migrations/022_add_db_size_rpc.sql` - `get_db_size_bytes` RPC for DB-size watchdog
   - `supabase/migrations/023_inactivate_dead_sources.sql` - Data cleanup: flip `is_active=false` on long-dead/never-produced sources
   - `supabase/migrations/024_strip_content_from_search_vector.sql` - Drop `content` from `search_vector` to shrink the GIN index
   - `supabase/migrations/025_drop_unused_indexes.sql` - Drop indexes with `idx_scan=0` to cut write amplification
   - `supabase/migrations/026_add_batch_content_update_rpc.sql` - `batch_update_article_content` RPC for batched content backfill
   - `supabase/migrations/027_security_hardening.sql` - Multi-finding hardening: `search_articles` explicit projection + 200-char input cap + 3s statement_timeout; SECURITY DEFINER funcs rebuilt with `search_path = ''` and in-function role check; column-level GRANT on `articles` (anon loses `url_hash` + backfill state); `articles_with_source` recreated with explicit projection; `source_health` + `get_db_size_bytes` revoked from anon (Edge Function uses service role internally); defence-in-depth REVOKE on `fetch_logs`
   - `supabase/migrations/028_search_articles_explicit_casts.sql` - Hotfix consolidation: replace `pg_catalog.least` with bare `LEAST` + add `::TEXT` casts on the VARCHAR(N) columns in `search_articles` RETURNS TABLE
   - `supabase/migrations/029_compress_articles_content_lz4.sql` - Switch `articles.content` TOAST compression from `pglz` to `lz4` (Supabase PG build supports `--with-lz4`); affects new writes only — existing rows rewrite naturally over the 7-day cleanup cycle, no `VACUUM FULL` (free-tier has no maintenance window)
   - `supabase/migrations/030_add_source_max_content_length.sql` - Optional per-source content length cap (`sources.max_content_length INT`, default NULL = use global). Worker clamps to `MIN(this, global maxContentLen)` at both the initial-parse site and the content backfill site — a misconfigured large value can't escape the global ceiling.
   - `supabase/migrations/031_prune_old_image_urls_rpc.sql` - `prune_old_image_urls(days_to_keep)` SECURITY DEFINER RPC: batched (5000/loop) NULL of `image_url` + `thumbnail_url` on articles older than `IMAGE_PRUNE_DAYS` (default 3). `IS NOT NULL` guard skips already-pruned rows. Called as a non-fatal third step in `runCleanup`; `GetArticlesNeedingOGImage` gets a matching age filter so the backfill workflow won't re-fetch what was just nulled. Uses `request.jwt.claims->>'role'` caller gate (CURRENT_USER is dead inside SECURITY DEFINER; SESSION_USER is always `authenticator` for PostgREST).
   - `supabase/migrations/032_prune_old_content_rpc.sql` - `prune_old_content(days_to_keep)` SECURITY DEFINER RPC: same shape as 031, nulls `articles.content` past `CONTENT_PRUNE_DAYS` (default 2). **Destructive to the iOS article-detail view for 2-7d articles** — iOS must handle NULL `content` (placeholder / "view on source" / summary fallback) before this lands. `GetArticlesNeedingContent` gets a matching age filter so backfill doesn't re-extract content for nulled rows.

This creates:
- `categories` table with 10 categories (including Podcasts & Videos)
- `sources` table with 133 feeds (articles, podcasts, YouTube channels)
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
git push -u origin master
```

### 5. Configure GitHub Secrets

Two scopes:

**Repo secrets** (Settings → Secrets and variables → Actions):

| Secret Name | Used by |
|-------------|---------|
| `SUPABASE_URL` | `fetch-rss.yml`, `cleanup.yml`, `backfill.yml`, `watchdog.yml` |
| `SUPABASE_SERVICE_ROLE_KEY` | `fetch-rss.yml`, `cleanup.yml`, `backfill.yml` (worker writes). `watchdog.yml` does **not** need it. |

**`production` Environment secrets** (Settings → Environments → New environment "production"):

| Secret Name | Used by |
|-------------|---------|
| `SUPABASE_ACCESS_TOKEN` | `deploy-functions.yml` |
| `SUPABASE_PROJECT_REF` | `deploy-functions.yml` |

Configure the `production` Environment with **required reviewers** (yourself) and **deployment branches: master only**. This gates every Edge Function deploy on a human approval click in the Actions UI and scopes the deploy secrets so they can't be exfiltrated from unrelated workflows.

### 6. Enable GitHub Actions

The workflows are already configured:
- `fetch-rss.yml` - Runs every 2 hours
- `cleanup.yml` - Runs daily at 3 AM UTC
- `backfill.yml` - og:image + content backfill daily at 04:30 UTC (manual `workflow_dispatch` accepts `kind: both|images|content`)
- `watchdog.yml` - Source-health check every 6 hours (fails the job + emails when circuit/stale/high-failure/DB-quota thresholds breach)

Go to **Actions** tab and enable workflows if prompted.

### 7. Deploy Edge Functions

Two paths:

**Manual (first deploy):**

```bash
# Install Supabase CLI
brew install supabase/tap/supabase

# Login and link project
supabase login
supabase link --project-ref YOUR_PROJECT_REF

# Deploy all functions
supabase functions deploy
```

**Automated (after the `production` Environment is set up):** any merge to `master` touching `supabase/functions/**` queues a deploy in `deploy-functions.yml`. The job pauses on the "Waiting" step until a maintainer clicks **Approve and deploy** in the Actions UI, then ships the bundle.

The Edge Functions provide a caching layer for the iOS app with Cache-Control headers. They also enforce request guards (column whitelist, `limit` clamp, `q` length cap, URL length cap, oversized-value drop) documented in `docs/api-reference.md`.

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
│   ├── api-reference.md               # Edge Function endpoints + request guards
│   ├── database-schema.md             # Schema reference
│   ├── ios-integration.md             # iOS app integration guide
│   ├── operations-runbook.md          # Day-2 ops + on-call notes
│   ├── privacy.md                     # Overall privacy posture (no end-user PII)
│   ├── lgpd-conformance.md            # LGPD (Brazil) — position + guard rails
│   ├── gdpr-conformance.md            # GDPR (EU) — position + guard rails
│   ├── ccpa-conformance.md            # CCPA / CPRA (California) — position + guard rails
│   ├── ropa.md                        # Record of Processing Activities
│   └── data-retention.md              # 7-day retention policy
├── supabase/
│   ├── config.toml                    # Edge Functions config
│   ├── migrations/
│   │   ├── 001_initial_schema.sql     # Core database schema
│   │   ├── 002_add_media_support.sql  # Podcast/video columns
│   │   ├── 003_add_podcast_video_sources.sql  # Curated sources
│   │   ├── 004_update_articles_with_source_view.sql  # Expose media in API
│   │   ├── 005_fix_security_issues.sql  # Harden RLS, view, function security
│   │   ├── 006_add_composite_indexes.sql  # Composite indexes for performance
│   │   ├── 007_add_language_support.sql  # Language column on sources & articles
│   │   ├── 008_add_pt_es_sources.sql    # Portuguese & Spanish RSS sources
│   │   ├── 009_add_more_pt_es_sources.sql  # More PT & ES sources
│   │   ├── 010_add_pt_es_podcasts_videos.sql  # PT & ES podcasts, videos, politics
│   │   ├── 011_revoke_cleanup_from_anon.sql   # Restrict cleanup function access
│   │   ├── 012_add_content_to_search_vector.sql  # Include content in full-text search
│   │   ├── 013_drop_fetch_interval_minutes.sql   # Remove unused column
│   │   ├── 014_add_batch_image_update_rpc.sql    # Batch image update RPC
│   │   ├── 015_add_fetch_interval_hours.sql      # Adaptive fetch frequency
│   │   ├── 016_denormalize_articles.sql          # Denormalize source/category
│   │   ├── 017_backfill_denormalized_articles.sql # Backfill denormalized columns
│   │   ├── 018_add_backfill_tracking.sql         # Attempt counters + cooldown RPC for backfills
│   │   ├── 019_add_source_fetch_state_columns.sql # etag, last_modified, consecutive_failures, circuit_open_until on sources
│   │   ├── 020_add_source_health_infra.sql       # batch_update_source_fetch_state RPC + source_health view
│   │   ├── 021_batch_cleanup_old_articles.sql    # Batch cleanup_old_articles + per-function statement_timeout
│   │   ├── 022_add_db_size_rpc.sql               # get_db_size_bytes RPC for DB-size watchdog
│   │   ├── 023_inactivate_dead_sources.sql       # Data cleanup: inactivate long-dead/never-produced sources
│   │   ├── 024_strip_content_from_search_vector.sql # Drop content from search_vector
│   │   ├── 025_drop_unused_indexes.sql           # Drop indexes with zero usage
│   │   ├── 026_add_batch_content_update_rpc.sql  # Batch content-update RPC
│   │   ├── 027_security_hardening.sql            # Audit-driven hardening (search_articles, RLS column grants, view projection, REVOKE source_health/get_db_size_bytes from anon)
│   │   ├── 028_search_articles_explicit_casts.sql # Hotfix: bare LEAST + ::TEXT casts on VARCHAR(N) cols in search_articles
│   │   ├── 029_compress_articles_content_lz4.sql # Switch articles.content TOAST compression pglz → lz4 (new writes; existing rows rewrite via 7d cleanup)
│   │   ├── 030_add_source_max_content_length.sql # Optional per-source content cap (sources.max_content_length INT); worker clamps to MIN(this, global) at parse + backfill
│   │   ├── 031_prune_old_image_urls_rpc.sql      # Batched NULL of image_url + thumbnail_url on stale articles (>IMAGE_PRUNE_DAYS); runCleanup step + matching age filter on backfill query
│   │   └── 032_prune_old_content_rpc.sql         # Batched NULL of articles.content past CONTENT_PRUNE_DAYS; destructive to iOS article-detail until iOS handles NULL content
│   └── functions/                     # Edge Functions (caching proxy)
│       ├── _shared/                   # Shared utilities, memory cache + tests
│       ├── api-categories/            # Categories endpoint (24h cache)
│       ├── api-sources/               # Sources endpoint (1h cache)
│       ├── api-articles/              # Articles endpoint (5min + ETag)
│       ├── api-search/                # Search endpoint (1min private)
│       ├── api-health/                # Health check endpoint (no-store)
│       └── api-source-health/         # Per-source fetch health + summary + DB size (60s cache)
├── rss-worker/
│   ├── go.mod                         # Go module definition
│   ├── main.go                        # Entry point
│   └── internal/
│       ├── config/                    # Configuration + tests
│       ├── models/                    # Data models + tests
│       ├── parser/                    # RSS parsing + tests
│       ├── database/                  # Supabase client + tests (with retry logic)
│       ├── httputil/                  # Shared HTTP transport + connection pooling
│       └── logger/                    # Structured logging with level support
└── .github/
    ├── workflows/
    │   ├── fetch-rss.yml              # RSS fetch job (every 2 hours)
    │   ├── cleanup.yml                # Cleanup job (daily)
    │   ├── test.yml                   # Unit tests + lint + govulncheck (on push/PR)
    │   ├── security.yml               # Secret scan, SAST, deps, SBOM (push/PR + weekly)
    │   ├── pr-checks.yml              # PR-only: title conventional-commits, go.mod sync, migration format
    │   ├── deploy-functions.yml       # Auto-deploy Edge Functions on push
    │   ├── watchdog.yml               # Source health check every 6h (fails job on degradation)
    │   ├── lgpd-conformance.yml       # LGPD guard rails (PII bans, doc gates, ops + structural)
    │   └── gdpr-conformance.yml       # GDPR + CCPA guard rails (same shape, EU/US patterns)
    ├── lgpd-gdpr-rules.toml           # Custom gitleaks rules: CPF, CNPJ, IBAN, US SSN
    ├── pii-allowlist.txt              # Allowed email literals (maintainer + reserved domains)
    └── dependabot.yml                 # Weekly dependency updates
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

### Portuguese Articles (20 sources)
| Source | Category |
|--------|----------|
| Folha de S.Paulo, G1 (Globo), BBC Brasil, UOL Noticias | World |
| Tecnoblog, Olhar Digital, Canaltech | Technology |
| InfoMoney, Exame, Valor Economico | Business |
| ge (Globo Esporte), Gazeta Esportiva, UOL Esporte | Sports |
| G1 Ciencia e Saude, Revista Galileu, Superinteressante | Science |
| Veja Saude, Metropoles Saude | Health |
| CinePOP, PapelPop | Entertainment |

### Portuguese Podcasts (10 sources)
| Source | Topic |
|--------|-------|
| Braincast, Hipsters Ponto Tech, Tecnocast | Technology |
| Cafe da Manha | News |
| NerdCast, Flow Podcast | Entertainment |
| Naruhodo, Dragoes de Garagem | Science |
| PrimoCast | Business |
| Xadrez Verbal | Politics |

### Portuguese Videos (10 sources)
| Source | Topic |
|--------|-------|
| TecMundo, Filipe Deschamps | Technology |
| Manual do Mundo, Nerdologia | Science |
| BBC News Brasil | News |
| Desimpedidos | Sports |
| Porta dos Fundos | Entertainment |
| Me Poupe!, O Primo Rico | Business |
| Drauzio Varella | Health |

### Portuguese Politics (3 sources)
| Source | Category |
|--------|----------|
| Poder360, Congresso em Foco, Folha de S.Paulo Poder | Politics |

### Spanish Articles (19 sources)
| Source | Category |
|--------|----------|
| El Pais, BBC Mundo, El Mundo, Infobae | World |
| Xataka, Hipertextual | Technology |
| Expansion, Cinco Dias, El Economista | Business |
| Marca, AS, Mundo Deportivo | Sports |
| Muy Interesante, National Geographic Espana | Science |
| 20 Minutos Salud | Health |
| SensaCine, Espinof, 20 Minutos Cine | Entertainment |

### Spanish Podcasts (10 sources)
| Source | Topic |
|--------|-------|
| Despeja la X | Technology |
| Radio Ambulante | News |
| Se Regalan Dudas, Nadie Sabe Nada, The Wild Project | Entertainment |
| TED en Espanol | Science |
| Entiende Tu Mente, Cristina Mitre | Health |
| El Partidazo de COPE | Sports |
| BBVA Blink | Business |

### Spanish Videos (10 sources)
| Source | Topic |
|--------|-------|
| Nate Gentile, Dot CSV | Technology |
| QuantumFracture, CdeCiencia | Science |
| BBC News Mundo, DW Espanol | News |
| Ibai, Luisito Comunica | Entertainment |
| Value School | Business |
| FisioOnline | Health |

### Spanish Politics (3 sources)
| Source | Category |
|--------|----------|
| elDiario.es Politica, La Vanguardia Politica, El Confidencial | Politics |

### Adding New Sources

1. Go to Supabase Dashboard → **Table Editor** → **sources**
2. Click **Insert row**
3. Fill in:
   - `name`: Display name
   - `slug`: Unique identifier (lowercase, hyphens)
   - `feed_url`: RSS feed URL
   - `category_id`: Select from categories table
   - `language`: ISO 639-1 code (e.g., `en`, `pt`, `es`) — defaults to `en`
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

# Filter by language
GET /api-articles?language=eq.en&order=published_at.desc&limit=20
GET /api-sources?language=eq.pt

# Search articles (1min private cache)
GET /api-search?q=climate&limit=20

# Health check (no caching)
GET /api-health

# Per-source fetch health + summary + DB size (60s cache) — powers the watchdog workflow
# Response includes a `database` block (size_bytes/size_pretty/quota_pct) via get_db_size_bytes RPC
GET /api-source-health
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
# Required
export SUPABASE_URL="https://your-project.supabase.co"
export SUPABASE_SERVICE_ROLE_KEY="your-service-role-key"

# Optional
export LOG_LEVEL=INFO                   # DEBUG, INFO (default), WARN, ERROR
export LOG_FORMAT=text                  # text (default) or json (for log aggregators)
export HOST_RATE_LIMIT_RPS=2.0          # per-host requests/sec for RSS/og:image/content
export HOST_RATE_LIMIT_BURST=5          # per-host burst allowance
export BACKFILL_MAX_ATTEMPTS=3          # retries before an article is excluded from backfill
export BACKFILL_COOLDOWN_HOURS=24       # min gap between backfill attempts on the same article
export CIRCUIT_FAILURE_THRESHOLD=5      # consecutive fetch failures before the circuit trips
export CIRCUIT_BASE_BACKOFF_HOURS=1     # initial cool-off window on trip; doubles per additional failure
export CIRCUIT_MAX_BACKOFF_HOURS=24     # cap on the exponential circuit backoff

# Edge Functions only (read by api-source-health):
export SUPABASE_DB_QUOTA_BYTES=524288000 # DB-size cap for quota_pct calculation (default 500 MB free tier)

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
make cleanup           # Remove articles older than 7 days (and same-age fetch_logs)
make backfill-images   # Fetch og:images for articles missing images
make backfill-content  # Extract content for articles

# Supabase Functions
make deploy            # Deploy all Edge Functions
make deploy-categories # Deploy api-categories
make deploy-sources    # Deploy api-sources
make deploy-articles   # Deploy api-articles
make deploy-search     # Deploy api-search
make deploy-health     # Deploy api-health
make functions-serve   # Run Edge Functions locally

# Utilities
make clean             # Remove build artifacts
```

## Monitoring

### Fetch Logs

Check the `fetch_logs` table in Supabase:
- `status`: running / completed / partial_failure / failed
- `articles_inserted`: New articles added
- `articles_skipped`: Duplicates skipped
- `errors`: Any errors encountered

### GitHub Actions

View workflow runs at:
`https://github.com/YOUR_USERNAME/pulse-backend/actions`

## Security

The `security.yml` workflow runs on every push/PR to `master` and weekly on Mondays (06:00 UTC) to catch newly disclosed CVEs in existing dependencies. Jobs:

| Job | Tool | What it catches |
|-----|------|-----------------|
| Secret Scan | gitleaks + TruffleHog | Leaked API keys, tokens, and credentials in code and full git history (TruffleHog validates against live APIs to cut false positives) |
| Go SAST | gosec | SQL injection, hardcoded credentials, weak crypto, unsafe HTTP clients, and other insecure Go patterns |
| Go Vulnerabilities | govulncheck | Known CVEs in Go module dependencies |
| Trivy Filesystem | Trivy | Dependency CVEs (all ecosystems), additional secret patterns, and misconfigurations in Dockerfiles / GitHub workflows / IaC |
| SBOM | Trivy (CycloneDX) | Generates a Software Bill of Materials as a workflow artifact for supply-chain audits |

All jobs run in parallel and fail the build on any finding. The weekly schedule ensures that vulnerabilities disclosed after merge still surface. Dependabot (weekly) handles automated dependency bumps for both Go modules and GitHub Actions.

## Workflow and Branch Protection

| Workflow | Trigger | Purpose |
|----------|---------|---------|
| `fetch-rss.yml` | Every 2 hours + manual | Fetch RSS feeds into Supabase |
| `cleanup.yml` | Daily 3 AM UTC + manual | Remove articles older than the retention window |
| `test.yml` | Push/PR to `master` | Go tests (race + coverage), **100% coverage gate** (fails if total `< 100.0%`), golangci-lint, govulncheck, Deno tests |
| `security.yml` | Push/PR to `master` + weekly Mon 06:00 UTC | Secret scan (gitleaks + TruffleHog), gosec, govulncheck, Trivy, CycloneDX SBOM |
| `pr-checks.yml` | PR to `master` only | PR title conventional-commits, `go.mod` sync, migration filename/format |
| `deploy-functions.yml` | Push to `master` touching `supabase/functions/**` | Build + deploy Edge Functions. Gated by the `production` Environment — pauses for required-reviewer approval before shipping. |
| `watchdog.yml` | Every 6 hours + manual | Polls `api-source-health`; fails job (→ GitHub email) on circuit/stale/high-failure/DB-quota threshold breach |
| `lgpd-conformance.yml` | Push/PR to `master` + weekly Mon 07:00 UTC | LGPD guard rails: CPF/CNPJ + SSN regex bans, required privacy docs, retention + RLS + no-PII-redaction invariant, structural integrity on migrations |
| `gdpr-conformance.yml` | Push/PR to `master` + weekly Mon 07:00 UTC | GDPR + CCPA guard rails: IBAN + EU-phone + SSN regex bans plus the same docs/operational/structural checks as the LGPD workflow |

Branch protection on `master` requires all 19 jobs across `test.yml`, `security.yml`, `pr-checks.yml`, `lgpd-conformance.yml`, and `gdpr-conformance.yml` to pass before merge. Direct pushes to `master` are blocked (even for admins); every change goes through a PR. Repo is configured with squash-only merges and `delete_branch_on_merge`.

## Data Protection Conformance

The repo asserts and enforces a **no-end-user-PII** posture: the backend aggregates public RSS news only and processes no personal data of identified or identifiable natural persons. Two parallel workflows act as living guard rails — substantive position documents live under `docs/`, and PRs that would erode the posture fail CI.

Position documents (one `last_reviewed:` header each):
- [`docs/privacy.md`](docs/privacy.md) — overall privacy posture.
- [`docs/lgpd-conformance.md`](docs/lgpd-conformance.md) — Brazilian LGPD assessment.
- [`docs/gdpr-conformance.md`](docs/gdpr-conformance.md) — European GDPR assessment.
- [`docs/ccpa-conformance.md`](docs/ccpa-conformance.md) — California CCPA / CPRA assessment.
- [`docs/ropa.md`](docs/ropa.md) — Record of Processing Activities (Art. 30 / LGPD Art. 37).
- [`docs/data-retention.md`](docs/data-retention.md) — 7-day retention policy.

Each conformance workflow runs four parallel jobs (`pii-scan`, `docs-presence`, `operational-controls`, `structural-integrity`) that enforce: regulator-specific PII regex bans (CPF/CNPJ, IBAN, EU phone, SSN); a small allowlist of permitted email literals (`.github/pii-allowlist.txt`); the absence of `RemoteAddr`/`X-Forwarded-For` in `rss-worker/`; cleanup wiring and `ArticleRetentionDays = 7`; no plaintext `http://` in migrations; the `No PII redaction layer required` invariant; RLS not disabled on user-facing tables; no new tables outside the `{categories, sources, articles, fetch_logs}` allowlist; no PII-implying column names; and the ROPA subprocessor table.

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

1. Reduce `ArticleRetentionDays` in config (default: 7)
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
