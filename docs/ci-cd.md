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
| `test.yml` | Push/PR to `master` | Go tests (race + coverage), **100% coverage gate** (fails if total `< 100.0%`), golangci-lint, govulncheck, Deno tests |
| `security.yml` | Push/PR to `master` + weekly Mon 06:00 UTC | Secret scan (gitleaks + TruffleHog), gosec, govulncheck, Trivy, CycloneDX SBOM |
| `codeql.yml` | Push/PR to `master` + weekly Mon 00:00 UTC | GitHub CodeQL static analysis; uploads SARIF to the Security tab |
| `pr-checks.yml` | PR to `master` only | PR title conventional-commits, `go.mod` sync, migration filename/format |
| `migrations-ci.yml` | Push/PR touching `supabase/migrations/**`, `supabase/config.toml`, or `supabase/tests/**` | Boots the local Supabase stack, applies all migrations from scratch (`supabase db reset --no-seed`), `supabase db lint --fail-on error`, then runs `supabase/tests/security_invariants.sql` |
| `lint-meta.yml` | Push/PR | `actionlint` (+ shellcheck on run-blocks) over all workflows |
| `deploy.yml` | Push to `master` touching `supabase/migrations/**`, `supabase/functions/**`, or `supabase/config.toml` + manual | Gated by the `production` Environment (required-reviewer approval). Ordered steps under `set -e`: apply migrations (`supabase db push`; no-ops if `SUPABASE_DB_PASSWORD` unset) → deploy Edge Functions → api-health smoke test. Concurrency group `deploy-production`, no cancel-in-progress. |
| `watchdog.yml` | Every 6 hours + manual | Polls `api-source-health`; fails job (→ GitHub email) on circuit/stale/high-failure/DB-quota threshold breach |
| `lgpd-conformance.yml` | Push/PR to `master` + weekly Mon 07:00 UTC | LGPD guard rails: CPF/CNPJ + SSN regex bans, required privacy docs, retention + RLS + no-PII-redaction invariant, structural integrity on migrations |
| `gdpr-conformance.yml` | Push/PR to `master` + weekly Mon 07:00 UTC | GDPR + CCPA guard rails: IBAN + EU-phone + SSN regex bans plus the same docs/operational/structural checks as the LGPD workflow |
| `claude.yml` | Issue/PR comments, reviews, issue events | On-demand Claude Code agent (restricted to repo owner/members/collaborators) |
| `claude-code-review.yml` | PR opened/synchronize/reopened (trusted authors) | Automated Claude Code review of PR diffs |

## Branch Protection

Branch protection on `master` requires all **19 jobs** across `test.yml` (3),
`security.yml` (5), `pr-checks.yml` (3), `lgpd-conformance.yml` (4), and
`gdpr-conformance.yml` (4) to pass before merge. Direct pushes to `master` are
blocked (even for admins); every change goes through a PR. The repo is configured
with squash-only merges and `delete_branch_on_merge`.

> `migrations-ci.yml`, `lint-meta.yml`, and `codeql.yml` run on every relevant PR
> but are not yet in the required-check set — promote them in branch protection
> once they've proven stable.

## Security

The `security.yml` workflow runs on every push/PR to `master` and weekly on
Mondays (06:00 UTC) to catch newly disclosed CVEs in existing dependencies.
CodeQL (`codeql.yml`) runs a parallel SAST pass and, together with gosec and
Trivy, uploads SARIF to the GitHub Security tab.

| Job | Tool | What it catches |
|-----|------|-----------------|
| Secret Scan | gitleaks + TruffleHog | Leaked API keys, tokens, and credentials in code and full git history (TruffleHog validates against live APIs to cut false positives) |
| Go SAST | gosec | SQL injection, hardcoded credentials, weak crypto, unsafe HTTP clients, and other insecure Go patterns |
| Go Vulnerabilities | govulncheck | Known CVEs in Go module dependencies |
| Trivy Filesystem | Trivy | Dependency CVEs (all ecosystems), additional secret patterns, and misconfigurations in Dockerfiles / GitHub workflows / IaC |
| SBOM | Trivy (CycloneDX) | Generates a Software Bill of Materials as a workflow artifact for supply-chain audits |

All jobs run in parallel and fail the build on any finding. The weekly schedule
ensures that vulnerabilities disclosed after merge still surface. Dependabot
(weekly) handles automated dependency bumps for both Go modules and GitHub
Actions.

## Secrets

- **Repo scope** — `SUPABASE_URL`, `SUPABASE_SERVICE_ROLE_KEY` (used by
  `fetch-rss.yml`, `cleanup.yml`, `backfill.yml`; the watchdog only needs
  `SUPABASE_URL`).
- **`production` Environment** — `SUPABASE_ACCESS_TOKEN`, `SUPABASE_PROJECT_REF`,
  `SUPABASE_DB_PASSWORD` (used by `deploy.yml` only; gated by required-reviewer
  approval). See [setup.md](setup.md) for the full secret-configuration steps.
