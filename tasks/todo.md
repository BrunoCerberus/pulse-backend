# Security Hardening (2026-05-14)

Resolves findings from the multi-surface security audit.

## Phase 1 — Edge Functions [DONE]

- [x] **H1 + H17** — `supabase-proxy.ts`: always apply `defaultSelect`; drop client `select`/empty params
- [x] **H2** — `api-articles`: clamp `limit` to ≤ 100 before forwarding
- [x] **H9 + M-4** — `memory-cache.ts`: bounded LRU (1024 entries); `buildCacheKey` derives key from sanitized URL
- [x] **H16** — `api-search`: reject `q.length > 200`
- [x] **M-1/M-2/M-3** — proxy: oversized values dropped (cap 256); `order` validated against allow-list; URL length cap 4096
- [x] **L-3** — `api-articles`: skip ETag/304 on non-200 upstream
- [x] **L-6 + L-7 + L-13** — `api-search` NaN-safe limit, clamped to [1, 100]
- [x] **L-1** — `cors.ts`: `Object.freeze(corsHeaders)`
- [x] **L-5** — `api-health`: drop `timestamp` (no clock fingerprint)
- [x] **M-8** — `config.toml`: declared `[functions.api-source-health] verify_jwt = false`
- [x] **C2** — `watchdog.yml`: dropped service-role auth headers; api-source-health now uses service-role internally
- [x] Tests: 123 Deno tests pass (added new tests for select-bypass, limit-clamp, 414, NaN handling, LRU eviction)

## Phase 2 — Go RSS worker [DONE]

- [x] **C3** — `parser.go`: wrap `resp.Body` with `io.LimitReader(50 MB)`
- [x] **C4** — `parser.go`: fresh `gofeed.Parser` per call + explicit translator fields
- [x] **H3 + H4 + H5** — `httputil.SafeTransport` with `SecureDialContext` (one resolve, no rebinding) + `IsForbiddenIP` + per-redirect re-validation
- [x] **H13** — `main.go`: `os.Unsetenv("SUPABASE_SERVICE_ROLE_KEY")` after `config.Load`
- [x] **M1** — `parser.go`: `html.UnescapeString` decodes numeric/hex entities before stripping
- [x] **M3** — `parser.go`: single-pass regex newline collapse (was O(n²))
- [x] **M4 + L5 + L12** — `parser.go`: scheme validation on article URL, media URL, thumbnail
- [x] **M5** — `ogimage.go`: reject internal-IP literal og:image URLs (forbidden ranges)
- [x] **M6** — `parser.go`: `parseSafeInt` with overflow guard; duration capped at 24h
- [x] **M7** — `config.go`: require `https://` (loopback http allowed for local dev)
- [x] **M8** — already plumbed (no change — `http.NewRequestWithContext` already used in the hot paths)
- [x] **L2** — `parser.go`: length caps on title (500), summary (4096), content (200K), author (200), URL (2048)
- [x] **L3** — `ogimage.go`: reject control chars in extracted URL
- [x] **L9** — `parser.go`: `canonicalizeURL` (strip fragment, lowercase scheme/host, sort query keys)
- [x] **L10** — `parser.go`: clamp `published_at` to `[10y ago, now+1h]`
- [x] **L11** — `parser.go`: `sanitizeText` strips C0/C1 control chars + bidi-override codepoints
- [x] Tests: 100% coverage on every Go package; race detector clean

## Phase 3 — DB migration 027 [DONE — pending apply]

`supabase/migrations/027_security_hardening.sql` consolidates:

- [x] **C1** — `search_articles` returns explicit projection (no SETOF articles); SECURITY DEFINER bypasses anon column-grants; 3s statement_timeout; 200-char input cap
- [x] **C5** — `batch_update_article_images`/`batch_update_article_content`/`bump_backfill_attempts`/`batch_update_source_fetch_state`/`cleanup_old_articles` all rewritten with `search_path = ''` + fully-qualified refs + in-function role check
- [x] **H6** — `REVOKE SELECT ON articles FROM anon/authenticated` + column-level `GRANT SELECT (safe-cols)`; `articles_with_source` recreated projecting only safe columns
- [x] **H7** — `REVOKE EXECUTE ON get_db_size_bytes FROM anon, authenticated`
- [x] **H8** — `REVOKE SELECT ON source_health FROM anon, authenticated`
- [x] **H14** — `cleanup_old_articles` has in-function `current_user` check
- [x] **M-1** — `search_articles` length cap + statement_timeout (above)
- [x] **fetch_logs** — defence-in-depth `REVOKE ALL FROM anon, authenticated`

## Phase 4 — CI/CD + repo hygiene [DONE]

- [x] **H10** — `deploy-functions.yml`: `environment: production` declared (you must create the Environment in GH UI for the gate to take effect)
- [x] **H11** — `claude.yml`: author-association gate on every trigger
- [x] **H12** — `claude-code-review.yml`: author-association gate + dropped `issues: write`
- [x] **M-1** — removed `go mod tidy` from `fetch-rss.yml`, `cleanup.yml`, `backfill.yml`
- [x] **M-2** — pinned `golangci-lint version: v2.4.0`
- [x] **M-3** — pinned `setup-deno deno-version: v1.46.3`
- [x] **M-4** — `deno.lock` committed; `deno test --frozen` enforces it in CI
- [x] **L-1** — `.claude/` added to `.gitignore`
- [x] **L-3** — `watchdog.yml`: integer-only validation before `(( ))`
- [x] **L-4** — removed duplicate cleanup job from `fetch-rss.yml` (now handled by `cleanup.yml` only)

## Phase 5 — Items requiring your action (cannot be automated)

These are blockers between the code landing and the protections being live.

- [ ] **Apply migration 027 to production** — `supabase db push` from the main repo (not a worktree, per memory note)
- [ ] **Deploy Edge Functions** — `supabase functions deploy --project-ref <ref>` (or merge to master to trigger `deploy-functions.yml` once the Environment is set up)
- [ ] **Create the `production` Environment in GitHub**: Settings → Environments → New environment "production". Add yourself as a required reviewer. Set deployment branches to `master` only. Move `SUPABASE_ACCESS_TOKEN` and `SUPABASE_PROJECT_REF` from repo secrets to this Environment so they're gated behind the approval rule.
- [ ] **Rotate `SUPABASE_SERVICE_ROLE_KEY`** — only AFTER the watchdog change deploys (otherwise the next watchdog run fails). The old key was being sent in `Authorization` headers to api-source-health on every 6h tick and may sit in Supabase function logs.
- [ ] **Verify branch protection still lists the 11 required checks** by name (`go-tests`, `lint`, `deno-tests`, `secret-scan`, `go-sast`, `go-vuln`, `trivy`, `sbom`, `pr-title`, `gomod-sync`, `migration-format`). The CI job names didn't change in this PR but it's worth a glance after the merge.

## Verification (all green locally)

- [x] `go test ./...` — 7 packages pass
- [x] `go test -race ./...` — race detector clean
- [x] `go test -coverprofile` — **100.0%** total coverage
- [x] `go vet ./...` — clean
- [x] `deno test --frozen` — 123 passed / 0 failed
- [x] Subprocess tests (`TestMainBinary_*`) — pass; confirms the unset-env + config.https-check + SSRF guard work in the spawned binary too

## Live re-probe (after deploy)

Run these against the prod URL to confirm the fixes landed:

```bash
BASE="https://wczntdipdtmhbrmrnryp.supabase.co/functions/v1"

# H1 — select bypass blocked (should NOT include content/created_at/etc)
curl -sS "$BASE/api-articles?select=*&limit=1" | python3 -m json.tool | head -25

# H2 — limit clamped (response should be <100 KB, not 800+)
curl -sS -o /dev/null -w "bytes=%{size_download}\n" "$BASE/api-articles?limit=999999"

# H16 — oversized q returns []
curl -sS "$BASE/api-search?q=$(python3 -c 'print("a"*201)')"

# 414 — oversized URL blocked
curl -sS -o /dev/null -w "HTTP %{http_code}\n" "$BASE/api-articles?slug=$(python3 -c 'print("x"*5000)')"

# api-source-health no longer needs auth
curl -sS -o /dev/null -w "HTTP %{http_code}\n" "$BASE/api-source-health"

# Direct PostgREST: backfill columns now revoked
curl -sS -H "apikey: <ANON-KEY>" "https://wczntdipdtmhbrmrnryp.supabase.co/rest/v1/articles?select=image_backfill_attempts&limit=1"
# Expected: error mentioning column-level permission
```
