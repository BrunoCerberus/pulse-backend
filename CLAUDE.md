# CLAUDE.md

Pulse Backend is a self-hosted news aggregation backend for the Pulse iOS app. Go 1.25 + Supabase + Deno Edge Functions.

## Quick Start

```bash
export SUPABASE_URL="https://your-project.supabase.co"
export SUPABASE_SERVICE_ROLE_KEY="your-service-role-key"

make build                # Build RSS worker
make run                  # Fetch feeds
make test                 # Run all tests (100% coverage enforced)
make deploy              # Deploy Edge Functions
supabase functions serve # Local testing
```

## Architecture

```
GitHub Actions (every 2h)
    ↓
Go RSS Worker
├─ Fetch 136 RSS feeds (adaptive intervals + circuit breaker)
├─ Parse with gofeed + enrich (og:image, content extraction)
├─ Extract media enclosures (audio/video URLs, duration)
└─ Batch insert to Supabase (50/batch, dedup via url_hash)
    ↓
PostgreSQL (articles, sources, categories, fetch_logs)
    ↓
Edge Functions (caching proxy)
├─ /api-categories (24h cache)
├─ /api-sources (1h cache)
├─ /api-articles (15min + ETag)
├─ /api-search (1min, private)
├─ /api-health (no-store)
└─ /api-source-health (60s, includes DB quota)
    ↓
Pulse iOS App
```

## File Structure

```
rss-worker/                           Main Go application (100% coverage)
├── main.go                           Routing: fetch, cleanup, backfill-images, backfill-content
└── internal/
    ├── config/                       Env config + validation
    ├── models/                       Article, Source, Category, FetchLog structs
    ├── parser/                       RSS parsing + enrichment orchestration
    ├── database/                     Supabase REST API client (retry logic, RPCs)
    ├── httputil/                     SharedTransport + SafeTransport (SSRF guards)
    └── logger/                       slog-backed with text/json output

supabase/
├── migrations/                       35 migrations covering schema, security, indexing
├── tests/
│   └── security_invariants.sql       11 automated security checks (CI)
└── functions/                        Edge Functions (TypeScript + Deno)
    ├── _shared/                      cache.ts, cors.ts, etag.ts, memory-cache.ts
    └── api-{categories,sources,articles,search,health,source-health}/index.ts

.github/workflows/
├── fetch-rss.yml (2h)               Main RSS worker
├── cleanup.yml (daily 3 AM UTC)     Articles + fetch_logs cleanup
├── backfill.yml (daily 4:30 AM UTC) og:image + content extraction
├── test.yml (push/PR)               100% coverage gate, govulncheck, race detector
├── security.yml (push/PR + weekly)  Secret scan, SAST (gosec), Trivy, SBOM
├── deploy.yml (gated)               Migrate → Functions → Health check
├── migrations-ci.yml (push/PR)      Apply from scratch + security-invariants
├── watchdog.yml (6h)                Circuit/stale/quota health check
└── {lgpd,gdpr}-conformance.yml      PII bans, doc gates, structural checks
```

## Key Components

**httputil** — SSRF-protected transports:
- `SafeTransport`: resolve-once, forbids IP literals (loopback/RFC1918/CGNAT/etc.), re-validates on redirect
- `SharedTransport`: trusted host (Supabase), no SSRF checks
- Used by `NewClient()`, `NewClientWithRedirectLimit()`, `NewRateLimitedClient()` (preferred for external hosts)

**parser** — Hostile RSS feed handling:
- Feed body: 50 MB limit via `io.LimitReader`
- URL sanitization: `isSafeArticleURL` rejects non-http(s), embedded userinfo, control/bidi runes, obfuscated IP literals
- HTML decode: `html.UnescapeString` + C0/C1/bidi control stripping
- Length caps: title 500 / summary 4096 / content 200K / author 200 / URL 2048
- Date clamping: [10y ago, now+1h]
- Media extraction: audio/video enclosures + iTunes duration (24h cap)

**database** — Supabase REST API client:
- No redirects (0 max) so service-role key can't leak via 3xx
- Retry: exponential backoff on 429/502/503/504 (max 3 retries)
- Batch inserts: 50 articles/batch with `on_conflict=url_hash`
- Adaptive fetch: `GetActiveSources()` filters by interval, last_fetched_at, circuit_open_until
- Circuit breaker: trips after 5 consecutive failures, exponential backoff capped at 24h

**Database Schema** — 35 migrations:
- **Tables:** articles (denormalized source/category, media fields, backfill state), sources (feed_url, etag, last_modified, consecutive_failures, circuit_open_until), categories (10 total), fetch_logs
- **Language support:** ISO 639-1 codes (en, pt, es); articles inherit from source
- **Media fields:** media_type, media_url, media_duration, media_mime_type
- **Backfill tracking:** image/content_backfill_attempts + last_attempt_at; cooldown: 24h, max attempts: 3
- **Functions (all SECURITY DEFINER with search_path=''):**
  - `search_articles()` — 200-char limit, 3s timeout, explicit projection (no leaks)
  - `cleanup_old_articles()` — 5k-row batches, 5min timeout
  - `batch_update_article_{images,content}()` — RPC-based batch updates
  - `batch_update_source_fetch_state()` — Persist etag/last_modified/circuit state per fetch
  - `get_db_size_bytes()` — DB quota check (default 500 MB cap)
  - `prune_old_{image_urls,content}()` — Age-based nulling (IMAGE_PRUNE_DAYS=3, CONTENT_PRUNE_DAYS=2)
- **Views:**
  - `articles_with_source` — Explicit projection, hides url_hash + backfill state
  - `source_health` — Circuit + failure counts + most_recent_article_at
- **RLS + Grants:** Column-level SELECT on articles (safe cols + search_vector); fetch_logs revoked from anon/authenticated

**Configuration** — Environment variables:
```
SUPABASE_URL                    (required)
SUPABASE_SERVICE_ROLE_KEY       (required, keep secret)
LOG_LEVEL                       DEBUG|INFO(default)|WARN|ERROR
LOG_FORMAT                      text(default)|json
HOST_RATE_LIMIT_RPS             Per-host req/s for RSS/og/content (default 2.0)
HOST_RATE_LIMIT_BURST           Per-host burst (default 5)
BACKFILL_MAX_ATTEMPTS           Before exclusion (default 3)
BACKFILL_COOLDOWN_HOURS         Gap between attempts (default 24)
CIRCUIT_FAILURE_THRESHOLD       Failures to trip (default 5)
CIRCUIT_BASE_BACKOFF_HOURS      Initial cool-off (default 1, doubles per failure)
CIRCUIT_MAX_BACKOFF_HOURS       Cap on backoff (default 24)
IMAGE_PRUNE_DAYS                Age to null image_url/thumbnail_url (default 3)
CONTENT_PRUNE_DAYS              Age to null content (default 2)
```

## Testing

**100% coverage enforced on all Go packages.** Defensive branches unreachable with real inputs (e.g. `json.Marshal` on static types) use package-level function vars that tests swap. Pattern: see `internal/models`, `internal/httputil`, etc.

| Package | Coverage | Notes |
|---------|----------|-------|
| internal/models | 100% | HashURL, ShouldFetch, CategoryName, NewArticle |
| internal/config | 100% | Env validation (RATE_LIMIT_*, BACKFILL_*, CIRCUIT_*) |
| internal/httputil | 100% | SSRF guards, RateLimitingTransport, redirect limits |
| internal/parser | 100% | RSS parsing, og:image, content extraction, itemToArticle sanitization |
| internal/database | 100% | Batch ops, RPCs, retry logic, GetActiveSources circuit filter |
| internal/logger | 100% | Level filtering, text/JSON format, field propagation |
| main | 100% | processSource, runFetch, buildSourceFetchState, runBackfill |

```bash
make test           # All tests
make test-go-cover  # Coverage report
make test-deno      # Edge Functions
```

## Code Style

- Go: `go fmt`, `go vet`, `golangci-lint` in CI
- HTTP clients: always use `httputil.NewRateLimitedClient()` for external hosts (SSRF guard + rate limiting)
- Structured logging: `logger.With(key, val, ...)` at per-source/article sites
- Table-driven tests required for 100% coverage
- Test hostile inputs: RSS feeds, article content, og:images treated as attacker-controlled
- Edge Functions: TypeScript + Deno (`deno fmt`, `deno lint` enforced)

## CI/CD

**Required checks on master (25 total):** test.yml (5), security.yml (3), pr-checks.yml (3), lgpd-conformance.yml (4), gdpr-conformance.yml (4), migrations-ci.yml, lint-meta.yml, CodeQL (2), Dependency Review, Generate SBOM, claude-review (advisory)

**Key workflows:**
- **test.yml** — Go race detector + 100% coverage gate (fails if < 100.0%), govulncheck, golangci-lint, Deno lint/fmt
- **security.yml** — gitleaks, gosec, govulncheck, Trivy filesystem scan, CycloneDX SBOM
- **migrations-ci.yml** — Apply from scratch, plpgsql lint, run security_invariants.sql (11 checks: RLS, SECURITY DEFINER search_path, JWT-claim caller gates, projection limits, column grants)
- **deploy.yml** — Gated by production environment (requires approval). Steps: migrate → functions → api-health smoke test. Fails if SUPABASE_DB_PASSWORD missing.
- **watchdog.yml** — Every 6h. Polls `api-source-health`, fails job on circuit_open_count/stale_count/high_failure_count/quota_pct > thresholds

**Secrets:**
- Repo: SUPABASE_URL, SUPABASE_SERVICE_ROLE_KEY (fetch-rss, cleanup, backfill)
- Production Environment: SUPABASE_ACCESS_TOKEN, SUPABASE_PROJECT_REF, SUPABASE_DB_PASSWORD (deploy only)

## Security Review Guidance

**Threat model:** All RSS feeds, article content, og:images, and media enclosures are **hostile, attacker-controlled input**. The 136 configured publishers are NOT trusted.

**Risk concentration (audit-driven):**
1. **httputil** — SSRF/DNS-rebind/open-redirect: resolve-once, forbidden-IP rejection, redirect re-validation
2. **parser** — Input limits/sanitization: body-size cap, HTML decode, rune caps, MIME/CRLF validation, URL canonicalization, obfuscated IP detection
3. **Supabase boundary** — SECURITY DEFINER + search_path='', JWT-claim caller gate, RLS, column-level GRANTs, view projections
4. **Edge Functions** — Request guards, bounded projections, parameter validation
5. **Workflows** — Secret handling, fork-PR input processing

Consult [`THREAT_MODEL.md`](THREAT_MODEL.md) for threat context and [`SECURITY.md`](SECURITY.md) for disclosure/triage. Keep THREAT_MODEL.md current with control changes.

## Data Protection

No end-user PII processed — public RSS news only. Author bylines covered under journalism exemption (GDPR Art. 85 / LGPD Art. 4 / CCPA §1798.145(k)).

Conformance docs: [`privacy.md`](docs/privacy.md), [`lgpd-conformance.md`](docs/lgpd-conformance.md), [`gdpr-conformance.md`](docs/gdpr-conformance.md), [`ccpa-conformance.md`](docs/ccpa-conformance.md), [`ropa.md`](docs/ropa.md), [`data-retention.md`](docs/data-retention.md)

**Adding PII:** Update (1) conformance doc, (2) pii-scan allowlist if intentional, (3) ropa.md if new subprocessor, (4) any relevant allowlist.

## Monitoring

Check `fetch_logs` table:
- `status`: running / completed / partial_failure / failed
- `articles_inserted`, `articles_skipped`, `errors`

View GitHub Actions logs: Repository → Actions tab
