# Development

Local development, build/run commands, testing, and the repository layout.
For first-time project setup (Supabase, secrets, deploy) see
[setup.md](setup.md).

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
export IMAGE_PRUNE_DAYS=3               # age (days) past which image_url/thumbnail_url are nulled by daily cleanup; must be >0 and <= 7 (ArticleRetentionDays)
export CONTENT_PRUNE_DAYS=2             # age (days) past which articles.content is nulled by daily cleanup; same bounds as IMAGE_PRUNE_DAYS

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

`IMAGE_PRUNE_DAYS` and `CONTENT_PRUNE_DAYS` are validated at startup (must be
`> 0` and `<= ArticleRetentionDays`, which is `7`); the worker exits with an
error otherwise.

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

## Testing

Unit tests cover the Go packages and the Deno Edge Functions. All Go packages are
held at **100% statement coverage**; `test.yml` fails the build if total coverage
drops below 100.0%. Defensive branches that can't fail with real inputs are made
reachable via package-level function vars (e.g. `jsonMarshal`, `randRead`) that
tests swap — follow that pattern when adding similar code.

```bash
make test           # All tests
make test-go-cover  # Go with coverage report
make test-deno      # Deno Edge Function tests
```

CI runs the same suite plus golangci-lint and govulncheck on every push/PR —
see [ci-cd.md](ci-cd.md).

## Repository Layout

```
pulse-backend/
├── README.md                          # Landing page + documentation index
├── CLAUDE.md                          # Canonical agent guidance (Claude Code + any AGENTS.md tool)
├── AGENTS.md                          # → symlink to CLAUDE.md (vendor-neutral agents.md alias)
├── SECURITY.md                        # Vulnerability disclosure policy
├── Makefile                           # Common commands (make help)
├── .gitignore
├── docs/
│   ├── setup.md                       # First-time setup walkthrough
│   ├── content-sources.md             # Full source catalog (136 feeds)
│   ├── development.md                 # This file
│   ├── ci-cd.md                       # Workflows, branch protection, security
│   ├── api-reference.md               # Edge Function endpoints + request guards
│   ├── database-schema.md             # Schema reference
│   ├── ios-integration.md             # iOS app integration guide
│   ├── operations-runbook.md          # Day-2 ops, monitoring + troubleshooting
│   ├── privacy.md                     # Overall privacy posture (no end-user PII)
│   ├── lgpd-conformance.md            # LGPD (Brazil) — position + guard rails
│   ├── gdpr-conformance.md            # GDPR (EU) — position + guard rails
│   ├── ccpa-conformance.md            # CCPA / CPRA (California) — position + guard rails
│   ├── ropa.md                        # Record of Processing Activities
│   └── data-retention.md              # 7-day retention policy
├── supabase/
│   ├── config.toml                    # Edge Functions config
│   ├── migrations/
│   │   └── 001_*.sql … 033_*.sql      # 33 ordered migrations — see setup.md for the annotated list
│   ├── functions/                     # Edge Functions (caching proxy)
│   │   ├── deno.json                  # Deno config (imports/tasks)
│   │   ├── deno.lock                  # Deno dependency lockfile
│   │   ├── _shared/                   # cors, cache, etag, memory-cache, supabase-proxy + tests
│   │   ├── api-categories/            # Categories endpoint (24h cache)
│   │   ├── api-sources/               # Sources endpoint (1h cache)
│   │   ├── api-articles/              # Articles endpoint (15min + ETag)
│   │   ├── api-search/                # Search endpoint (1min private)
│   │   ├── api-health/                # Health check endpoint (no-store)
│   │   └── api-source-health/         # Per-source fetch health + summary + DB size (60s cache)
│   └── tests/                         # SQL security-invariant assertions (run by migrations-ci.yml)
├── rss-worker/
│   ├── go.mod                         # Go module definition
│   ├── go.sum                         # Go dependency checksums
│   ├── main.go                        # Entry point
│   ├── main_test.go                   # main package tests (100% coverage)
│   ├── .golangci.yml                  # golangci-lint config
│   └── internal/
│       ├── config/                    # Configuration + tests
│       ├── models/                    # Data models + tests
│       ├── parser/                    # RSS parsing + enrichment + tests
│       ├── database/                  # Supabase client + tests (with retry logic)
│       ├── httputil/                  # HTTP transports: Shared + SSRF-safe + per-host rate limiting
│       └── logger/                    # Structured logging with level support
├── tasks/                             # Working notes / scratch (todo.md)
└── .github/
    ├── workflows/
    │   ├── fetch-rss.yml              # RSS fetch job (every 2 hours)
    │   ├── cleanup.yml                # Cleanup job (daily 3 AM UTC)
    │   ├── backfill.yml              # og:image + content backfill (daily 04:30 UTC)
    │   ├── test.yml                   # Unit tests + lint + govulncheck (push/PR)
    │   ├── security.yml               # Secret scan, SAST, deps, SBOM (push/PR + weekly)
    │   ├── codeql.yml                 # CodeQL static analysis (push/PR + weekly)
    │   ├── pr-checks.yml              # PR-only: title, go.mod sync, migration format
    │   ├── deploy.yml                 # Gated deploy: migrations → functions → api-health smoke test
    │   ├── migrations-ci.yml          # db reset from scratch + db lint + SQL security invariants
    │   ├── lint-meta.yml              # actionlint (+ shellcheck) over all workflows
    │   ├── watchdog.yml               # Source health check every 6h (fails job on degradation)
    │   ├── lgpd-conformance.yml       # LGPD guard rails
    │   ├── gdpr-conformance.yml       # GDPR + CCPA guard rails
    │   ├── claude.yml                 # On-demand Claude Code agent (owner/members/collaborators)
    │   └── claude-code-review.yml     # Automated Claude PR review (trusted authors)
    ├── ISSUE_TEMPLATE/                # Bug + feature issue forms + config
    ├── pull_request_template.md       # Default PR description template
    ├── CODEOWNERS                     # Review ownership
    ├── lgpd-gdpr-rules.toml           # Custom gitleaks rules: CPF, CNPJ, IBAN, US SSN
    ├── pii-allowlist.txt              # Allowed email literals (maintainer + reserved domains)
    └── dependabot.yml                 # Dependency updates (minor/patch grouped per ecosystem)
```
