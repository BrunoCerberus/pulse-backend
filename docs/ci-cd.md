# CI/CD, Workflows & Security

All GitHub Actions workflows, branch-protection rules, and the security-scanning
pipeline. For the deploy walkthrough see [setup.md](setup.md); for the
vulnerability-disclosure policy see [../SECURITY.md](../SECURITY.md).

## Workflows

| Workflow | Trigger | Purpose |
|----------|---------|---------|
| `fetch-rss.yml` | Every 2 hours + manual | Fetch RSS feeds into Supabase |
| `cleanup.yml` | Daily 3 AM UTC + manual | Remove articles older than the retention window |
| `backfill.yml` | Daily 04:30 UTC + manual (`kind: both\|images\|content`) | og:image + content backfill (two parallel jobs) |
| `test.yml` | Push/PR to `master` | Go tests (single `-race -coverprofile` pass), risk-tiered coverage (**100% exact** for `internal/parser` + `internal/httputil`, **98%** whole-worker floor), golangci-lint, Deno lint/fmt/tests + 90% Deno line-coverage floor. Committed fuzz seeds replay here as ordinary tests; new-input discovery is nightly. |
| `security.yml` | Push/PR to `master` + weekly Mon 06:00 UTC | Secret scan (gitleaks + TruffleHog), gosec, govulncheck, Trivy; CycloneDX SBOM on merged/scheduled/manual states (not ephemeral PR revisions) |
| `codeql.yml` | Push/PR to `master` + weekly Mon 00:00 UTC | GitHub CodeQL static analysis; uploads SARIF to the Security tab |
| `pr-checks.yml` | PR to `master` only | PR title conventional-commits, `go.mod` sync, migration filename/format |
| `migrations-ci.yml` | Push/PR touching `supabase/migrations/**`, `supabase/config.toml`, or `supabase/tests/**` | Two jobs. **Apply migrations + invariants**: boots the local Supabase stack, applies all migrations from scratch (`supabase db reset --no-seed`), replays this PR's new migrations **incrementally** on top of the base-branch schema (`supabase migration up`, the path production takes), `supabase db lint --fail-on error`, then runs `supabase/tests/security_invariants.sql`. **Edge Function contract tests** (also runs when `supabase/functions/**` changed): boots the local stack, applies all migrations, then hits all six endpoints over HTTP with the same status/shape/`Cache-Control` assertions as the production deploy smoke test — the functions are only otherwise verified in production. Both jobs are gated by a shared change-detection job and skipped-but-green otherwise |
| `lint-meta.yml` | Push/PR | `actionlint` (+ shellcheck on run-blocks) for correctness and `zizmor` for workflow security (template injection, credential persistence, permissions, action pinning) over all workflows and composite actions |
| `deploy.yml` | Push to `master` touching `supabase/migrations/**`, `supabase/functions/**`, or `supabase/config.toml` + manual | Gated by the `production` Environment (required-reviewer approval). Ordered steps under `set -e`: apply migrations (`supabase db push --dry-run` to log pending + surface drift, then the real push; **fails** if `SUPABASE_DB_PASSWORD` unset) → deploy Edge Functions → wait for `api-health` → smoke-test **all six endpoints** (status, response shape, `Cache-Control`). Concurrency group `deploy-production`, no cancel-in-progress. |
| `watchdog.yml` | Every 2 hours + manual | Polls `api-source-health`; fails on circuit/stale/high-failure/DB-quota thresholds or when the latest successful fetch is over 180 minutes old |
| `privacy-conformance.yml` | Push/PR to `master` + weekly Mon 07:00 UTC | Unified LGPD/GDPR/CCPA guard rails: jurisdiction-specific PII patterns plus shared docs, retention, RLS, no-PII, and structural invariants |
| `claude.yml` | Issue/PR comments, reviews, issue events | On-demand Claude Code agent (restricted to repo owner/members/collaborators) |
| `claude-code-review.yml` | PR opened/synchronize/reopened to `master` (trusted authors) | Advisory Claude Code review of PR diffs; cannot approve or request changes |
| `security-review.yml` | PR opened/synchronize/reopened to `master` (trusted authors) | Advisory AI security review anchored to `THREAT_MODEL.md`; never issues a merge verdict |
| `scorecard.yml` | Push to `master` + weekly Mon 05:30 UTC + branch-protection changes | OpenSSF Scorecard supply-chain posture score; SARIF to Security tab (informational) |
| `deno-deps.yml` | Weekly Mon 06:30 UTC + manual | Runs `deno outdated` over `supabase/functions` and keeps a single tracking issue in sync (opens/updates/closes it). Dependabot has no Deno ecosystem, so this is the Edge Functions' only dependency-update path. |
| `toolchain-freshness.yml` | Weekly Mon 06:15 UTC + manual | Freshness tracker for the security binaries pinned in env blocks (gitleaks, gosec, actionlint, zizmor) plus the Supabase CLI: Dependabot can't see versions in env blocks, so without this watch the pins rot silently. Advisory (never fails): keeps one tracking issue in sync with the outdated table, including where each pin + SHA256 must move. |
| `fuzz.yml` | Daily 02:00 UTC + manual (`fuzztime`, default `10m`) | Extended fuzzing: one matrix job per discovered `func FuzzXxx`, each with its own cached corpus (`~/.cache/go-build/fuzz`) so coverage compounds night over night. Failing inputs upload as `fuzz-crashers-<Target>`; commit them under `testdata/fuzz/<Target>/` as permanent seeds. |
| `keepalive.yml` | Monthly (1st, 05:45 UTC) + manual | Resets GitHub's 60-day scheduled-trigger inactivity timer so crons (fetch, cleanup, watchdog, …) survive commit-quiet periods |

## Branch Protection

Protection on `master` is a repository **ruleset** ("Master") requiring **20
deterministic status checks** before merge: `test.yml` (3), `security.yml` (4),
`pr-checks.yml` (4, incl. Dependency Review), `privacy-conformance.yml` (4),
`codeql.yml` (2), `migrations-ci.yml` (2, incl. `Edge Function contract tests`),
and `lint-meta.yml` (1). AI reviews and SBOM generation still report but are
not merge gates. Direct pushes to `master` are blocked; every change goes through a PR. Squash-only merges,
`delete_branch_on_merge`, linear history, strict up-to-date branches, and
required review-thread resolution. Repository admins can bypass via PR.

## Fuzzing

Statement coverage proves lines executed at least once. It
does not prove that hostile bytes leave those lines intact — and per
`THREAT_MODEL.md`, every feed, article page, and enclosure is attacker
controlled. Fuzzing (control **C-FUZZ**) closes that gap.

Targets live next to the code they exercise (`internal/parser/fuzz_test.go`,
`internal/httputil/fuzz_test.go`) and assert control invariants rather than
merely absence of panics. The nightly workflow **discovers** targets by grepping
for `func FuzzXxx`, so a new target enrols itself with no workflow edit and
discovery fails loudly if no targets exist.

| Where | Budget | Purpose |
|-------|--------|---------|
| `test.yml` → ordinary Go tests | deterministic | Replay every committed fuzz seed on each PR |
| `fuzz.yml` (nightly, one job per target) | 10m × target | The actual hunt, with a cached corpus that compounds across nights |

Seed corpora under `testdata/fuzz/` also replay as ordinary subtests in the
`Go Tests` job, so a fixed crasher stays fixed even when nightly fuzzing is
delayed. When a nightly run fails, download its `fuzz-crashers-<Target>`
artifact and commit the minimized input as a seed in the same PR as the fix.

## Security

The `security.yml` workflow runs on every push/PR to `master` and weekly on
Mondays (06:00 UTC) to catch newly disclosed CVEs in existing dependencies.
CodeQL (`codeql.yml`) runs a parallel SAST pass and, together with gosec and
Trivy, uploads SARIF to the GitHub Security tab.

| Job | Tool | What it catches |
|-----|------|-----------------|
| Secret Scan | gitleaks + TruffleHog | Leaked API keys, tokens, and credentials in code and full git history (TruffleHog validates against live APIs to cut false positives) |
| Go SAST | gosec | SQL injection, hardcoded credentials, weak crypto, unsafe HTTP clients, and other insecure Go patterns |
| Go Vulnerabilities | govulncheck | Known CVEs in Go module dependencies (the repo's single govulncheck run) |
| Trivy Filesystem | Trivy | Dependency CVEs (all ecosystems), additional secret patterns, and misconfigurations in Dockerfiles / GitHub workflows / IaC |
| SBOM | Trivy (CycloneDX) | Generates an artifact for merged, scheduled, and manually dispatched states |

The scanning jobs run in parallel and fail the build on findings; SBOM generation
is an artifact-producing post-merge/scheduled job. The weekly schedule
ensures that vulnerabilities disclosed after merge still surface. Dependabot
(weekly) handles automated dependency bumps for both Go modules and GitHub
Actions; Deno dependencies are outside Dependabot's ecosystem support and are
covered by `deno-deps.yml` instead.

Every `actions/checkout` in the repo sets `persist-credentials: false`, so the
job token is never left behind in `.git/config` for later steps (or any code
they execute) to read. `zizmor`'s `artipacked` audit in `lint-meta.yml` enforces
this on new workflows.

## Secrets

- **Repo scope** — `SUPABASE_URL`, `SUPABASE_SERVICE_ROLE_KEY` (used by
  `fetch-rss.yml`, `cleanup.yml`, `backfill.yml`; the watchdog only needs
  `SUPABASE_URL`).
- **`production` Environment** — `SUPABASE_ACCESS_TOKEN`, `SUPABASE_PROJECT_REF`,
  `SUPABASE_DB_PASSWORD` (used by `deploy.yml` only; gated by required-reviewer
  approval). See [setup.md](setup.md) for the full secret-configuration steps.
