# 15 Backend Improvements

## Completed Tasks

- [x] **Task 1:** Deno endpoint tests in CI — expanded test coverage to all endpoints
- [x] **Task 2:** Search vector includes content — migration 012 adds content at weight C
- [x] **Task 3:** fetch_logs cleanup — `CleanupOldFetchLogs()` runs during daily cleanup
- [x] **Task 4:** Edge Function CI/CD — `deploy-functions.yml` auto-deploys on push
- [x] ~~Task 5: Alerting~~ — SKIPPED (GitHub email notifications sufficient)
- [x] **Task 6:** Retry logic — `doWithRetry()` with exponential backoff on 429/5xx
- [x] **Task 7:** Go module/build caching — `cache: true` on all setup-go steps
- [x] **Task 8:** Update stale documentation — database-schema.md + operations-runbook.md
- [x] **Task 9:** Replace log.Fatalf in backfill — `runBackfill` returns error
- [x] **Task 10:** Linting in CI — golangci-lint job + .golangci.yml config
- [x] **Task 11:** Dependency vulnerability scanning — govulncheck + dependabot.yml
- [x] **Task 12:** Structured logging — logger package with level support (LOG_LEVEL env)
- [x] **Task 13:** Drop dead schema column — migration 013 drops fetch_interval_minutes
- [x] **Task 14:** Health check endpoint — api-health with tests, config.toml, Makefile
- [x] **Task 15:** cleanHTML fix — strips script/style tag contents before tag removal

## Verification

- All Go tests pass with race detector: 7/7 packages
- Coverage: main 80%, config 100%, database 78.8%, httputil 100%, logger 94.4%, models 100%, parser 93.2%
- `go vet ./...` clean
- Build succeeds

## Remaining Manual Steps

1. Apply migrations 012 and 013 via Supabase SQL Editor
2. Add GitHub secrets: `SUPABASE_ACCESS_TOKEN`, `SUPABASE_PROJECT_REF` (for deploy-functions.yml)
3. Push to branch and verify CI workflows pass
