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
- `supabase/migrations/001–036_*.sql` (applied in order, annotated history in `docs/setup.md`) ·
  `supabase/functions/` — Edge Functions + `_shared/` · `supabase/tests/security_invariants.sql`
- `scripts/check-endpoints.sh` — the shared six-endpoint contract suite run by the
  migrations-ci contract job AND the production deploy smoke test (one file, two callers)
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
C-WRITEREVOKE, C-FUZZ) across:

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

Go coverage is risk-tiered: `internal/parser` and `internal/httputil` require **100% exact statement
coverage**; everything outside that tier has a **98%** floor measured on its own statements, so the
exact tier can't subsidise it. `test.yml` sums the cover profile's own counts rather than trusting
`go tool cover -func`'s rounded display. Unreachable defensive branches (e.g. `json.Marshal` on
static types, `crypto/rand.Read`) are exercised via package-level function vars (`jsonMarshal` in
`internal/database`, `randRead` in `main`) swapped in tests — keep those seams and follow the
pattern for new similar code. The 98% floor is headroom for genuinely unreachable code, not licence
to delete the seams: doing so would consume the whole budget in one go.

Edge Functions have a **90% line-coverage floor** (currently ~98%), enforced in the same workflow.
Go runs as a single `go test -race -coverprofile` pass — don't split it back into two runs.

**Coverage is not the same as robustness.** Statement coverage proves each line ran once, not
that hostile bytes leave it intact — so the hostile-input surfaces also carry **fuzz targets**
(control C-FUZZ) in `internal/parser/fuzz_test.go` and `internal/httputil/fuzz_test.go`. Assert
control invariants there, not just absence of panics. `fuzz.yml` (nightly) discovers targets by
grepping for `func FuzzXxx`, so a new target needs no workflow edit — but it is the only workflow
that searches for new inputs; on a PR a target contributes only its committed `testdata/fuzz/` seeds,
which replay as ordinary subtests. When a target finds a crasher,
commit the minimized `testdata/fuzz/` input as a seed in the same PR as the fix — seeds replay as
ordinary subtests on every `go test`.

| Package | Key Tests |
|---------|-----------|
| `internal/models` | HashURL, NewArticle, ShouldFetch, CategoryName |
| `internal/config` | Load + all env var validation |
| `internal/httputil` | All transports, redirect cap, rate limiting, ctx-cancel, SSRF |
| `internal/parser` | cleanHTML, OG image, content extraction, itemToArticle, ParseFeed (200/304/non-2xx), parseDuration |
| `internal/database` | Batch inserts/images/content/state, circuit filter, retry, error branches |
| `internal/logger` | Level filtering, text+JSON, With(), nil fallbacks, Fatalf |
| `main` | processSource (panic recovery), runFetch, circuit helpers, runBackfill, every main() command |
| fuzz (`internal/{parser,httputil}`) | cleanHTML, sanitizeText, canonicalizeURL, URL safety, parseDuration, truncateToFirstParagraph, IsForbiddenIP, ValidateSSRFTarget |
| `_shared/*.ts` | cache, cors, etag utilities |

Commands: `make test` · `make test-go-cover` · `make test-deno` · `make fuzz` (all targets, 30s each).

## Code Style Guidelines

- `go fmt`, `go vet`, `golangci-lint` in CI. `deno fmt` + `deno lint` for Edge Functions.
- Table-driven tests; mock HTTP with `httptest.Server`.
- **All new HTTP clients must use `httputil.NewClient`, `NewClientWithRedirectLimit`, or
  `NewRateLimitedClient`** (preferred for external hosts) — never `http.DefaultClient`.
- `logger.With(key, val)` for per-source/article structured logs; `logger.Infof` for one-off summaries.
- No comments unless the WHY is non-obvious.
- **New Deno dependencies go in `supabase/functions/deno.json`'s `imports` map** (JSR specifiers,
  e.g. `jsr:@std/assert`) — never a bare URL import in a `.ts` file. Dependabot has no Deno
  ecosystem, so `deno-deps.yml`'s weekly `deno outdated` sweep is the only thing watching these,
  and it can only see what the import map declares. A bare URL still compiles and still passes
  tests — it just silently stops being monitored, which is how the tree ended up pinned to a
  2023 `deno.land/std` for years.
- Keep the local Deno version in step with `test.yml`'s pin (currently **v2.9.4**). `deno fmt`
  output shifts between minor releases, so drift shows up only as a CI format failure.
  `test.yml`'s pin is the canonical one — `deno-deps.yml` reads it rather than carrying a copy.

## GitHub Actions / CI

Full workflow list, branch protection, and the security-scanning pipeline: `docs/ci-cd.md`.

When editing any workflow, two things are enforced by `lint-meta.yml` (a required check) —
`actionlint` + shellcheck for correctness, `zizmor` for security:

- **Every `actions/checkout` must set `persist-credentials: false`** (C-NOCRED), so the job token
  isn't left in `.git/config` for later steps, or the code they execute, to read.
- Pin third-party actions to a commit SHA; verify curl-installed binaries against a pinned SHA256
  before extracting.

`claude-code-review.yml` and `security-review.yml` are **self-locking**: `claude-code-action`
refuses to run from a workflow file that differs from the one on `master`, so a PR that edits
either shows a failed run until it merges. Expected. Both reviews are advisory and neither is a
required check, so the red run does not block the merge — don't "fix" it by reverting the edit.

## Data Protection Conformance

No end-user PII processed — public RSS news only. `author` bylines: journalism exemption (GDPR
Art. 85 / LGPD Art. 4 § II / CCPA §1798.145(k)). Adding personal-data processing requires updating
the relevant conformance doc, pii-scan allowlist, `docs/ropa.md`, and structural-integrity
allowlists. Full position: `docs/privacy.md`.

## Monitoring

`fetch_logs` table + `api-source-health` endpoint (circuit/stale/high-failure counts, latest
successful fetch age, DB `quota_pct`; watchdog trips at 180 minutes / 60%). Full runbook:
`docs/operations-runbook.md`.
