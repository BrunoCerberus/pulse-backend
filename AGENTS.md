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
make cleanup           # Remove articles older than 7 days (and same-age fetch_logs)
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
│       │   └── transport.go           # SharedTransport (Supabase) + SafeTransport (SSRF-aware DialContext) + RateLimitingTransport (per-host token bucket); IsForbiddenIP/ValidateSSRFTarget guards
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
│   │   ├── 020_add_source_health_infra.sql        # batch_update_source_fetch_state RPC + source_health view
│   │   ├── 021_batch_cleanup_old_articles.sql     # Batch cleanup_old_articles + raise per-function statement_timeout
│   │   ├── 022_add_db_size_rpc.sql                # get_db_size_bytes RPC for DB-size watchdog
│   │   ├── 023_inactivate_dead_sources.sql        # Data cleanup: inactivate long-dead/never-produced sources
│   │   ├── 024_strip_content_from_search_vector.sql # Drop content from search_vector
│   │   ├── 025_drop_unused_indexes.sql            # Drop indexes with zero usage
│   │   ├── 026_add_batch_content_update_rpc.sql   # Batch content-update RPC
│   │   ├── 027_security_hardening.sql             # Audit-driven hardening: explicit search_articles projection, search_path='' on SECURITY DEFINER funcs, column-level GRANT on articles, view re-projection, source_health/get_db_size_bytes revoked from anon
│   │   ├── 028_search_articles_explicit_casts.sql # Hotfix: bare LEAST + ::TEXT casts on VARCHAR(N) cols in search_articles
│   │   ├── 029_compress_articles_content_lz4.sql  # Switch articles.content TOAST compression pglz → lz4 (new writes only; existing rows rewrite via 7d cleanup cycle, no VACUUM FULL)
│   │   └── 030_add_source_max_content_length.sql  # Optional per-source content cap (sources.max_content_length INT); worker clamps to MIN(this, global) at parse + backfill
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
│       └── api-source-health/index.ts # Per-source fetch health + summary + DB size (60s cache)
├── .github/
│   ├── workflows/
│   │   ├── fetch-rss.yml              # Runs every 2 hours
│   │   ├── cleanup.yml                # Runs daily at 3 AM UTC
│   │   ├── backfill.yml               # Daily backfill (og:images + content) at 04:30 UTC
│   │   ├── test.yml                   # Unit tests + lint + govulncheck on push/PR
│   │   ├── security.yml               # Secret scan, SAST, deps, SBOM (push/PR + weekly)
│   │   ├── pr-checks.yml              # PR-only: title conventional-commits, go.mod sync, migration format
│   │   ├── deploy-functions.yml       # Auto-deploy Edge Functions on push
│   │   ├── watchdog.yml               # Source health check every 6h (fails job on degradation)
│   │   ├── lgpd-conformance.yml       # LGPD guard rails (PII bans, doc gates, ops + structural)
│   │   └── gdpr-conformance.yml       # GDPR + CCPA guard rails (same shape, EU/US patterns)
│   ├── lgpd-gdpr-rules.toml           # Custom gitleaks rules: CPF, CNPJ, IBAN, US SSN
│   ├── pii-allowlist.txt              # Allowed email literals (maintainer + RFC 6761 reserved)
│   └── dependabot.yml                 # Weekly dependency updates
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
| HTTP Utils | `internal/httputil/transport.go` | Two base transports: `SharedTransport` (Supabase, trusted single host) and `SafeTransport` (user-content, SSRF-aware via `SecureDialContext` + `IsForbiddenIP` — rejects loopback/RFC1918/link-local/multicast/unspecified at the dial layer). Clients: `NewClient`, `NewClientWithRedirectLimit` (both Shared), and `NewRateLimitedClient` (SafeTransport + per-host token bucket + per-redirect SSRF re-validation) |
| Logger | `internal/logger/logger.go` | slog-backed: LOG_LEVEL gates emission; LOG_FORMAT=text (default, slog.TextHandler) or json (slog.JSONHandler). Printf-style `Debugf/Infof/Warnf/Errorf/Fatalf` plus `With(k, v, ...)` returning a sub-*slog.Logger for structured correlation |

### Edge Functions (`supabase/functions/`)

| Endpoint | Cache | Description |
|----------|-------|-------------|
| `/api-categories` | 24h public | Static category list |
| `/api-sources` | 1h public | RSS source list |
| `/api-articles` | 5min + ETag | Article feed with 304 support |
| `/api-search` | 1min private | Full-text search via RPC |
| `/api-health` | no-store | Liveness probe — returns `{"status":"ok"}` only (no clock fingerprint) |
| `/api-source-health` | 60s public | Per-source fetch health + aggregate summary + DB size block (size_bytes/size_pretty/quota_pct via `get_db_size_bytes` RPC; default 500 MB cap via `SUPABASE_DB_QUOTA_BYTES`); watchdog.yml polls this |

## Testing

Tests use Go's standard testing package with `httptest` for mocking HTTP calls, and Deno's built-in test runner for Edge Functions. **All Go packages are held at 100% statement coverage** and `test.yml` enforces it on every push/PR — new code that drops total coverage below 100.0% fails the `Go Tests` job. When you hit a defensive branch that can't fail with real inputs (e.g. `json.Marshal` on statically-typed payloads, `crypto/rand.Read`), make it reachable via a package-level function variable that tests swap — see `jsonMarshal` in `internal/database/supabase.go` and `randRead` in `main.go`.

| Package | Coverage | Description |
|---------|----------|-------------|
| `internal/models` | 100% | HashURL, NewArticle, ShouldFetch, CategoryName |
| `internal/config` | 100% | Env var loading + defaults (HOST_RATE_LIMIT_*, BACKFILL_*, CIRCUIT_*); `https://` validation on `SUPABASE_URL` with loopback http exemption for dev/tests; `isLoopbackHTTP` table-driven |
| `internal/httputil` | 100% | SharedTransport, SafeTransport, NewClient, NewClientWithRedirectLimit, RateLimitingTransport (per-host serialization, cross-host independence, ctx-cancel short-circuit, nil-base defaults to SafeTransport, zero-maxRedirects path), `IsForbiddenIP` table-driven (loopback toggle, private/RFC1918/link-local/ULA/multicast/unspecified), `ValidateSSRFTarget` (bad scheme, empty host, IP-literal blocked/allowed, DNS error / DNS-resolves-forbidden / DNS-resolves-allowed, parse failure), `SecureDialContext` (bad address, IP-literal forbidden, lookup error, empty IPs, forbidden resolved IP, allowed dial), redirect-time re-validation |
| `internal/parser` | 100% | HTML cleaning (partial-tag + no-closing-tag + numeric-entity decode edges), image extraction, OG/content fetching (body-read + readability errors), itemToArticle (embedded category, unsafe URL drop, oversized URL drop, unsafe thumbnail drop), ParseFeed (200/304/non-2xx + conditional-GET + bad-URL + transport errors + 50 MB body cap), parseDuration (HH:MM:SS + overflow guard + 24h cap + too-many-parts), `isSafeArticleURL`, `canonicalizeURL` (fragment drop / lowercase / sorted query / parse-error passthrough), `clampPublishedDate` (past / future / in-range), `extractAuthor` (Authors[] fallback, empty-after-sanitize), `sanitizeMIMEType` (CRLF reject), `extractMediaInfo` (unsafe URL / bad MIME), `sanitizeText` (control/bidi strip + truncate), `isControlOrBidi`, `truncRunes` (rune boundary), `parseSafeInt` (overflow), `isAcceptableOGImage` (control chars / scheme / IP literal / parse failure) |
| `internal/database` | 100% | Batch inserts, batch image RPC, BatchUpdateSourceFetchState, GetActiveSources circuit filter, retry logic, BumpBackfillAttempts, plus bad-URL/transport/marshal/decode error branches across every method |
| `internal/logger` | 100% | Level filtering, text + JSON output, `With()` field propagation (thread-safe via atomic.Pointer), nil-atomic fallbacks, subprocess-driven Fatalf |
| `main` | 100% | processSource (+ panic recovery), runFetch, nextCircuitOpenUntil, buildSourceFetchState, runBackfill, newRunID fallback, plus subprocess-driven TestMain covering every command (fetch/cleanup/backfill-images/backfill-content + config-load and runtime-error paths) |
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

Edge Function env vars (read by `api-source-health`):
- `SUPABASE_DB_QUOTA_BYTES` - DB-size cap used to compute `quota_pct` in the `database` block (default `524288000` = 500 MB free tier). Invalid/empty values fall back to the default.

Defaults in `internal/config/config.go`:
- `MaxConcurrent`: 5 sources processed simultaneously
- `ArticleRetentionDays`: 7 days (also drives fetch_logs retention via `CleanupOldFetchLogs`)

Graceful shutdown: `main()` installs `signal.NotifyContext(SIGINT, SIGTERM)` and threads that baseCtx into every run* command, so GHA cancellations and runner rotations exit cleanly instead of orphaning batches.

## Database Schema

Tables:
- `categories` - 10 categories (including Podcasts & Videos)
- `sources` - 133 pre-configured feeds with `fetch_interval_hours` (default 2, podcasts/videos 6). Migration 019 adds `etag`/`last_modified` (conditional GET validators) and `consecutive_failures`/`circuit_open_until` (circuit breaker state).
- `articles` - News articles with full-text search (tsvector), media fields, denormalized source/category columns, and backfill tracking (`image_backfill_attempts`, `image_backfill_last_attempt_at`, `content_backfill_attempts`, `content_backfill_last_attempt_at` — migration 018)
- `fetch_logs` - Monitoring records

Key functions (all SECURITY DEFINER use `SET search_path = ''` + qualified refs + in-function `CURRENT_USER` check after migration 027):
- `cleanup_old_articles(days_to_keep)` - Remove old articles (service_role only)
- `search_articles(search_query, result_limit)` - Full-text search; explicit TABLE projection (no SETOF articles), 200-char input cap, 3s statement_timeout, SECURITY DEFINER (bypasses anon column grants)
- `batch_update_article_images(updates)` - Batch image URL updates (service_role only)
- `batch_update_article_content(updates)` - Batch content updates (migration 026, service_role only)
- `bump_backfill_attempts(url_hashes, kind)` - Increments attempt counter + stamps `last_attempt_at`; `kind` is `"image"` or `"content"` (migration 018, service_role only, 10K array cap)
- `batch_update_source_fetch_state(updates)` - One round-trip per fetch cycle; JSONB array of per-source state (etag, last_modified, consecutive_failures, circuit_open_until, last_fetched_at) — migration 020 (service_role only)
- `get_db_size_bytes()` - Returns `pg_database_size(current_database())`; service_role only after migration 027 (Edge Function calls it with service-role key internally)

Views:
- `articles_with_source` - Explicit projection of `articles` (migration 027 dropped `url_hash` and backfill state from the view). `security_invoker=on`. Anon reads safe columns; iOS app uses this view.
- `source_health` - Per-source health snapshot (circuit_open, consecutive_failures, most_recent_article_at, articles_last_24h); `security_invoker=on`. Revoked from anon after migration 027 — `api-source-health` Edge Function authenticates upstream as service_role.

RLS + grants:
- `articles`: column-level `GRANT SELECT (safe-cols)` to anon/authenticated after migration 027; backfill columns + `url_hash` are service_role only.
- `fetch_logs`: anon/authenticated have nothing (REVOKE belt-and-suspenders on top of RLS).
- `categories`, `sources`: unchanged anon SELECT.

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
| `backfill.yml` | Daily 04:30 UTC + manual | og:image and content backfill (two parallel jobs); workflow_dispatch input picks one or both |
| `test.yml` | On push/PR | Go tests (race + coverage), **100% coverage gate**, golangci-lint, govulncheck, Deno tests |
| `security.yml` | On push/PR + weekly Mon 06:00 UTC | gitleaks + TruffleHog (secrets), gosec (Go SAST), govulncheck, Trivy (deps/secrets/misconfig), CycloneDX SBOM |
| `pr-checks.yml` | On PR to master only | PR title conventional-commits, go.mod Sync (`go mod tidy` must be a no-op), Migration Format (NNN_*.sql, no gaps, no duplicate prefixes) |
| `deploy-functions.yml` | On push to master under `supabase/functions/**` | Build + deploy Edge Functions. Gated by the `production` Environment (required reviewer + master-only branch rule); pauses for human approval in the Actions UI before shipping |
| `watchdog.yml` | Every 6 hours + manual | Polls `api-source-health`; fails job (→ GitHub email) when circuit/stale/high-failure counts or `database.quota_pct` exceed thresholds |
| `lgpd-conformance.yml` | Push/PR + weekly Mon 07:00 UTC | LGPD guard rails: CPF/CNPJ + SSN regex bans, email allowlist, IP-handling code ban, gitleaks history sweep, required-docs gates, retention + RLS + no-PII-redaction invariant, structural integrity on migrations. `cancel-in-progress` concurrency on PRs |
| `gdpr-conformance.yml` | Push/PR + weekly Mon 07:00 UTC | GDPR + CCPA guard rails: IBAN + EU/EEA phone + SSN regex bans plus the same docs/operational/structural checks as the LGPD workflow |

Branch protection on `master` requires all 19 checks across `test.yml`, `security.yml`, `pr-checks.yml`, `lgpd-conformance.yml`, and `gdpr-conformance.yml` to pass before merge. Direct pushes to `master` are blocked (including for admins); every change goes through a PR. Merge strategy is squash-only with `delete_branch_on_merge` enabled.

## Data Protection Conformance

The backend asserts and enforces a no-end-user-PII posture: public RSS news only, no personal data of identified or identifiable natural persons. The `author` byline on articles is treated under the journalism exemption (GDPR Art. 85 / LGPD Art. 4 § II / CCPA §1798.145(k)).

- **Position docs** live under `docs/`: `privacy.md` (overall posture), `lgpd-conformance.md` (Brazil), `gdpr-conformance.md` (EU), `ccpa-conformance.md` (California), `ropa.md` (Art. 30 / LGPD Art. 37 ROPA), `data-retention.md` (7-day window + cleanup mechanism). Each carries a `last_reviewed:` header.
- **Guard rails** are enforced by `lgpd-conformance.yml` and `gdpr-conformance.yml`. Each runs four parallel jobs: `pii-scan`, `docs-presence`, `operational-controls`, `structural-integrity`. CCPA's only distinctive identifier (US SSN) is enforced by an extra step in each existing `pii-scan` job rather than a third parallel workflow — chosen to limit drift between near-identical workflow files.
- **Shared inputs**: `.github/lgpd-gdpr-rules.toml` (gitleaks custom rules: CPF, CNPJ, IBAN, US SSN; path allowlist mirrors the regex exclusion lists in the workflows) and `.github/pii-allowlist.txt` (allowed email literals; comparison is case-insensitive).
- **When adding personal-data processing**: update (1) the relevant conformance doc, (2) the regex exclusion / allowlist if the addition is intentional, (3) `docs/ropa.md` if a new subprocessor is involved, and (4) the structural-integrity table allowlist or PII-column regex if a new table / column is added.

Secrets — split by scope so deploy credentials sit behind the Environment approval:

- **Repo secrets** (Settings → Secrets and variables → Actions): `SUPABASE_URL`, `SUPABASE_SERVICE_ROLE_KEY` — used by `fetch-rss.yml`, `cleanup.yml`, `backfill.yml`. `watchdog.yml` only needs `SUPABASE_URL` (the Edge Function reads service-role from auto-injected project env internally).
- **`production` Environment secrets** (Settings → Environments → production): `SUPABASE_ACCESS_TOKEN`, `SUPABASE_PROJECT_REF` — used by `deploy-functions.yml` only.
