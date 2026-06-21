# CLAUDE.md

Guidance for Claude Code and AI coding agents (also reachable via `AGENTS.md` symlink).

## Project Overview

Pulse Backend is a self-hosted news aggregation backend for the Pulse iOS app.

**Tech Stack:** Go 1.25 | Supabase (PostgreSQL + PostgREST) | GitHub Actions | Deno (Edge Functions)

## Architecture

```
GitHub Actions (every 2 hours)
    ↓
Go RSS Worker (rss-worker/)
    ├─ Fetch RSS feeds (136 sources, adaptive intervals)
    ├─ Parse with gofeed; enrich og:image (5 workers) + content (3 workers)
    ├─ Extract media enclosures (audio/video URLs, duration)
    └─ Batch insert to Supabase (50/batch, dedup via url_hash)
        ↓
PostgreSQL (articles, sources, categories, fetch_logs)
        ↓
Edge Functions (caching proxy + in-memory cache)
    ├── /api-categories    → 24h + 1h memory
    ├── /api-sources       → 1h + 30min memory
    ├── /api-articles      → 15min + ETag
    ├── /api-search        → 1min private
    ├── /api-health        → no-store
    └── /api-source-health → 60s (health + DB size)
        ↓
Pulse iOS App
```

## Build and Run Commands

```bash
export SUPABASE_URL="https://your-project.supabase.co"
export SUPABASE_SERVICE_ROLE_KEY="your-service-role-key"

make build             # Build the RSS worker binary
make run               # Run the RSS worker (fetch feeds)
make cleanup           # Remove articles older than 7 days
make backfill-images   # Fetch og:images for articles missing images
make backfill-content  # Extract full content for articles

make test              # Run all tests (Go + Deno)
make test-go           # Run Go tests
make test-go-cover     # Run Go tests with coverage
make test-go-race      # Run Go tests with race detector
make test-deno         # Run Deno Edge Function tests

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
├── Makefile
├── SECURITY.md / THREAT_MODEL.md / PATCHING.md
├── rss-worker/
│   ├── main.go                        # Entry point: fetch / cleanup / backfill-images / backfill-content
│   └── internal/
│       ├── config/config.go           # Env config
│       ├── models/models.go           # Article, Source, Category, FetchLog, FetchResult
│       ├── parser/
│       │   ├── parser.go              # RSS parsing + enrichment orchestration
│       │   ├── ogimage.go             # og:image extraction
│       │   └── content.go             # Full-content extraction (go-readability)
│       ├── database/supabase.go       # Supabase REST API client
│       ├── httputil/transport.go      # SharedTransport / SafeTransport / RateLimitingTransport
│       └── logger/logger.go           # slog-backed logger
├── supabase/
│   ├── migrations/001–035_*.sql       # Applied in order; see migration summaries below
│   ├── tests/security_invariants.sql  # 11 SQL invariants asserted in migrations-ci.yml
│   └── functions/
│       ├── _shared/                   # cors.ts, cache.ts, etag.ts, memory-cache.ts, supabase-proxy.ts
│       └── api-{categories,sources,articles,search,health,source-health}/index.ts
├── .github/
│   ├── workflows/                     # See GitHub Actions section
│   ├── dependabot.yml
│   ├── lgpd-gdpr-rules.toml           # Custom gitleaks rules
│   └── pii-allowlist.txt
└── docs/
    └── api-reference.md, database-schema.md, ios-integration.md,
        operations-runbook.md, privacy.md, lgpd-conformance.md,
        gdpr-conformance.md, ccpa-conformance.md, ropa.md, data-retention.md
```

### Migration history (001–035)
001 initial schema · 002 media fields · 003 podcast/video sources · 004 articles_with_source view · 005 security hardening · 006 composite indexes · 007 language support · 008–010 PT/ES sources+podcasts · 011 revoke cleanup from anon · 012 content in search_vector · 013 drop fetch_interval_minutes · 014 batch_image_update RPC · 015 fetch_interval_hours · 016–017 denormalize + backfill · 018 backfill tracking · 019 source fetch state (etag/circuit) · 020 source_health view + batch RPC · 021 batch cleanup + statement_timeout · 022 get_db_size_bytes · 023 inactivate dead sources · 024 strip content from search_vector · 025 drop unused indexes · 026 batch_content_update RPC · 027 security hardening (explicit projection, search_path='', column grants, source_health revoked from anon) · 028 explicit casts in search_articles · 029 LZ4 compression for content · 030 per-source max_content_length · 031 prune_old_image_urls RPC · 032 prune_old_content RPC · 033 fix SECURITY DEFINER caller gate (JWT-claim replaces dead CURRENT_USER) · 034 restrict sources columns (anon sees only public cols) · 035 REVOKE INSERT/UPDATE/DELETE on sources+categories from PUBLIC/anon/authenticated

## Key Components

### HTTP Utilities (`internal/httputil/transport.go`)
- **`SharedTransport`** — Supabase client only (single trusted host).
- **`SafeTransport`** — all external (user-content) clients. `DialContext` resolves host once, rejects forbidden IPs (`IsForbiddenIP`: loopback / RFC 1918 / 169.254/16 / multicast / unspecified / `0.0.0.0/8` / CGNAT / NAT64 translation prefixes / Class-E / TEST-NET), dials the IP literal to prevent DNS rebinding.
- **Client constructors**: `NewClient(timeout)` · `NewClientWithRedirectLimit(timeout, n)` · `NewRateLimitedClient(timeout, rps, burst, maxRedirects)` — the last wraps `SafeTransport` + per-host token bucket + redirect-time SSRF re-validation. All external RSS/og:image/content fetches must use `NewRateLimitedClient`.
- **Test knob**: `SetAllowLoopback(bool)` exempts loopback for `httptest.Server`; call via `TestMain`, never in production.

### Parser (`internal/parser/`)
- Feed body limited to 50 MB (`io.LimitReader`). Each `ParseFeed` allocates a fresh `gofeed.Parser` (avoids lazy-init race).
- **URL storage guard** (`isSafeArticleURL`): http(s)-only, non-empty host, no userinfo (`trusted.com@evil/` spoof), no control/bidi runes, length cap, `hostIsForbiddenIPLiteral` (handles decimal/hex/octal obfuscated IPs `net.ParseIP` misses). All four URL sinks (link, thumbnail, media, og:image) share this guard.
- `itemToArticle`: canonicalize URL (drop fragment, lowercase scheme/host, sort query splitting on `&` only), clamp `published_at` to `[10y ago, now+1h]`, decode HTML entities via `html.UnescapeString`, strip C0/C1 + bidi-override codepoints (incl. U+061C). Length caps: title 500 / summary 4096 / content 200K / author 200 / URL 2048.
- `ogimage.go`: 100KB head limit, delegates URL validation to unified guard. Fetch via `SafeTransport`.
- `content.go`: `go-readability`, 5 MB response limit, `MaxElemsToParse=100K`. Fetch via `SafeTransport`.
- `sanitizeMIMEType` (media): tight pattern, prevents CRLF injection. `parseDuration` uses overflow-safe `parseSafeInt`, caps at 24h.
- Rate limit: `parser.SetHostRateLimit(rps, burst)` at startup sets package-level vars; `New()`, `NewOGImageExtractor()`, `NewContentExtractor()` pick them up. Defaults: `DefaultHostRPS=2.0`, `DefaultHostBurst=5`.

### Database Client (`internal/database/supabase.go`)
- **Refuses redirects** (`NewClientWithRedirectLimit(30s, 0)`) — prevents apikey header forwarding on 3xx (PostgREST never legitimately redirects).
- Retry with exponential backoff on 429/502/503/504 (up to 3 retries).
- Batch inserts: 50/batch, `on_conflict=url_hash` + `ignore-duplicates`.
- Key methods: `GetActiveSources()` · `InsertArticles()` · `BatchUpdateArticleImages()` · `BatchUpdateArticleContent()` · `BatchUpdateSourceFetchState()` · `CleanupOldArticles()` · `GetArticlesNeedingOGImage()` · `GetArticlesNeedingContent()` · `BumpBackfillAttempts(urlHashes, kind)`.
- `GetActiveSources()` filters `fetch_interval_hours`, `last_fetched_at`, and `or=(circuit_open_until.is.null,circuit_open_until.lt.{now})`.

### Data Models (`internal/models/models.go`)
- `Source`: `FetchIntervalHours`, `EmbeddedCategory`, `ShouldFetch()`, `ETag`, `LastModified`, `ConsecutiveFailures`, `CircuitOpenUntil`.
- `Article`: media fields + denormalized `SourceName`, `SourceSlug`, `CategoryName`, `CategorySlug`.
- `NewArticle(language)` — articles inherit language from source.
- `HashURL()` — SHA256-based dedup key.
- `FetchResult`: includes `ETag`, `LastModified`, `NotModified` for per-source state persistence.

### Edge Functions (`supabase/functions/`)
- Shared proxy (`_shared/supabase-proxy.ts`) forces `select`, caps value length, allow-lists `order` columns, supports `ProxyConfig.paramValidators` (`isUuidFilter`/`isBooleanFilter`/`isSlugFilter`) that drop malformed values before DB/cache.
- Memory-cached endpoints skip empty result sets (`isCacheableResult`) to prevent LRU thrashing.
- `api-source-health` validates `id`/`slug`/`is_active` before any service-role DB call.

| Endpoint | Cache | Notes |
|----------|-------|-------|
| `/api-categories` | 24h public | Static list |
| `/api-sources` | 1h public | Public columns only (034) |
| `/api-articles` | 15min + ETag | 304 support |
| `/api-search` | 1min private | search_articles RPC |
| `/api-health` | no-store | `{"status":"ok"}` only |
| `/api-source-health` | 60s public | health + DB size (service-role internally) |

## Database Schema

**Tables:** `categories` (10) · `sources` (136) · `articles` · `fetch_logs`

**Key columns:**
- `sources.language` / `articles.language`: ISO 639-1, inherited at insert
- `articles`: `media_type`, `media_url`, `media_duration`, `media_mime_type`
- `articles`: denormalized `source_name`, `source_slug`, `category_name`, `category_slug`
- `sources.fetch_interval_hours`: default 2, podcasts/videos = 6
- Backfill tracking: `image_backfill_attempts`, `image_backfill_last_attempt_at`, `content_backfill_attempts`, `content_backfill_last_attempt_at`
- Circuit breaker: `consecutive_failures`, `circuit_open_until`; conditional-GET: `etag`, `last_modified`

**Key SQL functions** (all SECURITY DEFINER, `SET search_path = ''`, JWT-claim caller gate via `request.jwt.claims->>'role'`):
- `cleanup_old_articles(days_to_keep)` — batched 5,000-row deletes, 5min timeout. Service-role only.
- `search_articles(query, limit)` — explicit projection (no url_hash/backfill leak), rejects empty/>200-char, 3s timeout. Granted to anon/authenticated.
- `batch_update_article_images/content`, `bump_backfill_attempts`, `batch_update_source_fetch_state` — service-role only.
- `prune_old_image_urls(days)` / `prune_old_content(days)` — service-role only, batched NULL-out.
- `get_db_size_bytes()` — service-role only.

**Views:**
- `articles_with_source`: `security_invoker=on`, drops `url_hash`/backfill state. iOS reads through this.
- `source_health`: `security_invoker=on`, revoked from anon/authenticated (migration 027).

**RLS + grants (post-027/034/035):**
- `articles`: column-level GRANT to anon/authenticated (safe cols + search_vector only).
- `fetch_logs`: REVOKE ALL from anon/authenticated.
- `sources`: GRANT only public columns (id/name/slug/website_url/logo_url/category_id/language/is_active) to anon; operational + circuit-breaker cols are service-role only.
- `sources`/`categories`: REVOKE INSERT/UPDATE/DELETE from PUBLIC/anon/authenticated.

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `SUPABASE_URL` | — | Required |
| `SUPABASE_SERVICE_ROLE_KEY` | — | Required |
| `LOG_LEVEL` | INFO | DEBUG/INFO/WARN/ERROR |
| `LOG_FORMAT` | text | text or json |
| `HOST_RATE_LIMIT_RPS` | 2.0 | Per-host RPS for RSS/og:image/content clients |
| `HOST_RATE_LIMIT_BURST` | 5 | Per-host burst |
| `BACKFILL_MAX_ATTEMPTS` | 3 | Max retries before article excluded from backfill |
| `BACKFILL_COOLDOWN_HOURS` | 24 | Min gap between backfill attempts |
| `CIRCUIT_FAILURE_THRESHOLD` | 5 | Consecutive failures before circuit trips |
| `CIRCUIT_BASE_BACKOFF_HOURS` | 1 | Initial cool-off; doubles per extra failure |
| `CIRCUIT_MAX_BACKOFF_HOURS` | 24 | Backoff cap |
| `IMAGE_PRUNE_DAYS` | 3 | Days after which image_url/thumbnail_url are nulled; must be > 0 and ≤ ArticleRetentionDays |
| `CONTENT_PRUNE_DAYS` | 2 | Days after which articles.content is nulled; same bounds |

Config defaults: `MaxConcurrent=5`, `ArticleRetentionDays=7`. Graceful shutdown via `signal.NotifyContext` (SIGINT/SIGTERM).

## Testing

**100% statement coverage is required for all Go packages** — `test.yml` fails if total coverage < 100.0%. Unreachable defensive branches (e.g. `json.Marshal` on static types, `crypto/rand.Read`) are exercised via package-level function vars (`jsonMarshal`, `randRead`) swapped in tests. Follow this pattern for new similar code.

| Package | Key Tests |
|---------|-----------|
| `internal/models` | HashURL, NewArticle, ShouldFetch, CategoryName |
| `internal/config` | Load + all env var validation |
| `internal/httputil` | All transports, redirect cap, rate limiting, ctx-cancel, SSRF |
| `internal/parser` | cleanHTML, OG image, content extraction, itemToArticle, ParseFeed (200/304/non-2xx), parseDuration |
| `internal/database` | Batch inserts/images/content/state, circuit filter, retry, error branches |
| `internal/logger` | Level filtering, text+JSON, With(), nil fallbacks, Fatalf |
| `main` | processSource (panic recovery), runFetch, circuit helpers, runBackfill, every main() command |
| `_shared/*.ts` | cache, cors, etag utilities |

```bash
make test           # All tests
make test-go-cover  # Go with coverage report
make test-deno      # Deno Edge Function tests
```

## Code Style Guidelines

- `go fmt`, `go vet`, `golangci-lint` in CI. `deno fmt` + `deno lint` for Edge Functions.
- Table-driven tests; 100% statement coverage enforced.
- Mock HTTP with `httptest.Server`.
- **All new HTTP clients must use `httputil.NewClient`, `NewClientWithRedirectLimit`, or `NewRateLimitedClient`** (preferred for external hosts) — never `http.DefaultClient`.
- `logger.With(key, val)` for per-source/article structured logs; `logger.Infof` for one-off summaries.
- No comments unless the WHY is non-obvious.

## GitHub Actions

| Workflow | Trigger | Purpose |
|----------|---------|---------|
| `fetch-rss.yml` | Every 2h + manual | Run Go RSS worker |
| `cleanup.yml` | Daily 03:00 UTC + manual | Delete articles > 7d |
| `backfill.yml` | Daily 04:30 UTC + manual | Drain missing og:image + content (kind: both/images/content) |
| `test.yml` | Push/PR to master | Go tests (race + coverage), golangci-lint, govulncheck, Deno lint+tests |
| `security.yml` | Push/PR + weekly Mon 06:00 | gitleaks, TruffleHog, gosec, govulncheck, Trivy, SBOM |
| `security-review.yml` | PR (trusted authors) | AI security review; reads THREAT_MODEL.md; advisory only |
| `pr-checks.yml` | PR to master | Conventional-commit title, go.mod sync, migration format |
| `deploy.yml` | Push to master (migrations/** or functions/**) + manual | migrate → deploy functions → api-health smoke test; gated by `production` env |
| `migrations-ci.yml` | Push/PR touching migrations/** | `supabase db reset` → lint → 11 security invariants |
| `lint-meta.yml` | Push/PR + manual | actionlint + shellcheck on all workflows |
| `watchdog.yml` | Every 6h + manual | Fails on circuit_open/stale/high_failure/quota_pct > thresholds |
| `lgpd-conformance.yml` | Push/PR + weekly Mon 07:00 | PII bans, doc presence, retention, structural integrity |
| `gdpr-conformance.yml` | Push/PR + weekly Mon 07:00 | Same shape; EU/US PII patterns + CCPA SSN |

**Branch protection**: 25 required contexts; direct pushes to `master` blocked; squash-only merges. `security-review.yml` is advisory (non-required).

**Secrets:**
- Repo scope: `SUPABASE_URL`, `SUPABASE_SERVICE_ROLE_KEY`
- `production` environment: `SUPABASE_ACCESS_TOKEN`, `SUPABASE_PROJECT_REF`, `SUPABASE_DB_PASSWORD`

## Security Review Guidance

Threat model: [`THREAT_MODEL.md`](THREAT_MODEL.md) · Disclosure: [`SECURITY.md`](SECURITY.md) · Fix workflow: [`PATCHING.md`](PATCHING.md)

**Every RSS feed, article page, and media enclosure is hostile attacker-controlled input.** Review focus:
- `internal/httputil` — SSRF resolve-once, forbidden-IP rejection, redirect re-validation
- `internal/parser` — body-size caps, URL safety + canonicalization, bidi/control codepoints, date clamping, overflow guards
- Supabase privilege boundary — `SECURITY DEFINER` + `search_path=''`, JWT-claim caller gate, RLS, column grants, view projections
- Edge Function request guards and public API surface
- Workflow changes handling secrets or ingesting fork-PR input

Keep `THREAT_MODEL.md` current in the same PR that changes any control above.

## Data Protection Conformance

No end-user PII processed — public RSS news only. `author` bylines: journalism exemption (GDPR Art. 85 / LGPD Art. 4 § II / CCPA §1798.145(k)).

Adding personal-data processing requires updating: the relevant conformance doc, pii-scan exclusion/allowlist, `docs/ropa.md` (if new subprocessor), and any structural-integrity allowlists.

## Monitoring

- `fetch_logs` table: `status` (running/completed/partial_failure/failed), `articles_inserted`, `articles_skipped`, `errors`
- GitHub Actions → Actions tab
- `api-source-health` endpoint: circuit/stale/high-failure counts + DB quota_pct (watchdog trips at 60%)
