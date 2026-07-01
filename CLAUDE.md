# CLAUDE.md

Guidance for Claude Code and AI coding agents (also reachable via `AGENTS.md` symlink).

This file is deliberately short: it orients and points into `docs/` and `THREAT_MODEL.md` for
detail rather than duplicating it (those files are more thorough and easier to keep in sync than
a second copy here). If what you need isn't below, follow the pointer before grepping the code.

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

## Where things live

- `rss-worker/` — Go worker (`go run . [cleanup|backfill-images|backfill-content]`);
  `internal/{config,models,parser,database,httputil,logger}`
- `supabase/migrations/001–035_*.sql` (applied in order, annotated history in `docs/setup.md`) ·
  `supabase/functions/` — Edge Functions + `_shared/` · `supabase/tests/security_invariants.sql`
- `docs/` — full reference docs (annotated repo tree: `docs/development.md`)

Full command list + env vars: `docs/development.md`. First-time setup: `docs/setup.md`.

```bash
make test / make test-go-cover / make build / make run / make cleanup
cd rss-worker && go test -v ./...
```

## Security-critical code — read THREAT_MODEL.md first

**Every RSS feed, article page, and media enclosure is hostile, attacker-controlled input.**
`THREAT_MODEL.md` documents every control by ID (C-SSRF, C-URLSAFE, C-SANITIZE, C-LIMITS, C-CANON,
C-CLAMP, C-RATELIMIT, C-CIRCUIT, C-GRANT, C-VIEW, C-DEFINER, C-CALLERGATE, C-SEARCHCAP, C-BATCHCAP,
C-WRITEREVOKE) across:

- `internal/httputil` — SSRF resolve-once, forbidden-IP rejection, redirect re-validation
- `internal/parser` — body-size caps, URL safety/canonicalization, bidi/control stripping, date
  clamping, overflow guards
- Supabase — `SECURITY DEFINER` + `search_path=''`, JWT-claim caller gate, RLS, column grants,
  view projections
- Edge Functions — shared proxy guards; the proxy is a narrowing layer only, never a widening one

**Keep `THREAT_MODEL.md` current in the same PR that changes any control above.**
Disclosure process: `SECURITY.md` · Fix workflow: `PATCHING.md`.

## Method/field inventory (quick reference, not in THREAT_MODEL.md)

- `internal/database/supabase.go`: `GetActiveSources()` · `InsertArticles()` (retries
  429/502/503/504) · `BatchUpdateArticleImages/Content()` · `BatchUpdateSourceFetchState()` ·
  `CleanupOldArticles()` · `GetArticlesNeedingOGImage/Content()` · `BumpBackfillAttempts()`
- `internal/models/models.go`: `Source.ShouldFetch()` · `Article.HashURL()` ·
  `NewArticle(language)` · `FetchResult{ETag, LastModified, NotModified}`

## Database schema

4 tables: `categories` · `sources` (136) · `articles` · `fetch_logs`. Full column list, indexes,
views, SQL functions, and the RLS/grants matrix: `docs/database-schema.md`.

## Edge Functions

| Endpoint | Cache |
|----------|-------|
| `/api-categories` | 24h public |
| `/api-sources` | 1h public (public columns only) |
| `/api-articles` | 15min + ETag |
| `/api-search` | 1min private |
| `/api-health` | no-store |
| `/api-source-health` | 60s public (service-role internally) |

Full request/response contracts: `docs/api-reference.md`.

## Testing

**100% statement coverage is required for all Go packages** — `test.yml` fails if total coverage
< 100.0%. Unreachable defensive branches (e.g. `json.Marshal` on static types, `crypto/rand.Read`)
are exercised via package-level function vars (`jsonMarshal`, `randRead`) swapped in tests. Follow
this pattern for new similar code.

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

Commands: `make test` · `make test-go-cover` · `make test-deno`.

## Code Style Guidelines

- `go fmt`, `go vet`, `golangci-lint` in CI. `deno fmt` + `deno lint` for Edge Functions.
- Table-driven tests; mock HTTP with `httptest.Server`.
- **All new HTTP clients must use `httputil.NewClient`, `NewClientWithRedirectLimit`, or
  `NewRateLimitedClient`** (preferred for external hosts) — never `http.DefaultClient`.
- `logger.With(key, val)` for per-source/article structured logs; `logger.Infof` for one-off summaries.
- No comments unless the WHY is non-obvious.

## GitHub Actions / CI

Full workflow list, branch protection, and the security-scanning pipeline: `docs/ci-cd.md`.

## Data Protection Conformance

No end-user PII processed — public RSS news only. `author` bylines: journalism exemption (GDPR
Art. 85 / LGPD Art. 4 § II / CCPA §1798.145(k)). Adding personal-data processing requires updating
the relevant conformance doc, pii-scan allowlist, `docs/ropa.md`, and structural-integrity
allowlists. Full position: `docs/privacy.md`.

## Monitoring

`fetch_logs` table + `api-source-health` endpoint (circuit/stale/high-failure counts, DB
`quota_pct`; watchdog trips at 60%). Full runbook: `docs/operations-runbook.md`.
