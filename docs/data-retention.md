---
last_reviewed: 2026-05-14
---

# Data Retention Policy — Pulse Backend

Pulse Backend retains every operational and content record for **7 days**.
After that, it is purged by a daily cleanup workflow. No long-term
archives are kept.

This is the operational backbone behind the conformance posture
documented in [`docs/privacy.md`](./privacy.md),
[`docs/lgpd-conformance.md`](./lgpd-conformance.md), and
[`docs/gdpr-conformance.md`](./gdpr-conformance.md).

## What is retained, and for how long

| Data                       | Retention period | Purge mechanism                                          |
|----------------------------|------------------|----------------------------------------------------------|
| `articles` rows            | 7 days           | `cleanup_old_articles(7)` Postgres function, called daily. |
| `fetch_logs` rows          | 7 days           | Same cleanup pass (rows with `created_at < now() - interval '7 days'`). |
| Backups                    | Provider-managed (Supabase PITR) | Ephemeral; rolling window, not under maintainer control. |
| GitHub Actions logs        | 90 days (GitHub default) | Auto-purged by GitHub. Contain run summaries; no end-user data. |
| Edge Function in-memory cache | 1–60 minutes | TTL in `supabase/functions/_shared/memory-cache.ts`. Resets on Edge worker recycle. |

## Cleanup mechanism

1. `.github/workflows/cleanup.yml` runs daily at 03:00 UTC.
2. It invokes `./rss-worker cleanup`.
3. The Go worker calls the Supabase `cleanup_old_articles(days_to_keep)`
   RPC defined in `supabase/migrations/021_batch_cleanup_old_articles.sql`,
   which deletes both `articles` and `fetch_logs` rows older than the
   threshold in 5,000-row batches (statement timeout 5 minutes).
4. `ArticleRetentionDays = 7` in `rss-worker/internal/config/config.go`
   is the canonical retention literal. The conformance workflows assert
   that this file contains the literal `7` and that this document
   contains the literal `7 days`.

## Why 7 days

Three drivers, in priority order:

1. **Data minimisation.** Retaining articles longer than necessary serves
   no operational purpose. The iOS client only displays recent news; the
   backend does not run analytics, training, or historical search over
   the article corpus.
2. **Cost.** The project runs on Supabase's free tier (500 MB database
   quota; watchdog alerts at 60% — see `.github/workflows/watchdog.yml`).
   Seven days keeps utilisation comfortably below the alert threshold.
3. **News relevance.** Articles older than a week are stale enough that
   the iOS client's UX assumes they will not appear. The cleanup window
   matches that UX assumption.

## Exceptions

None. There is no opt-out tier, no maintainer-only extended retention,
no per-source override. If a source publisher takes content down, it
will roll out of the index naturally within seven days.

## Logs that fall outside this policy

- **GitHub Actions run logs** — retained per GitHub's default 90-day
  window. They contain run identifiers, source counts, error stacks, and
  similar operational signals. They do **not** contain end-user
  personal data because the system does not process any.
- **Supabase platform metrics / DB-level logs** — managed by Supabase
  per their platform policy. The maintainer does not configure
  application-level logging into these.

## Triggers for re-evaluation

Revisit this document if:

- The `ArticleRetentionDays` constant changes.
- A new table is added that warrants its own retention period.
- An external request (LGPD ANPD, GDPR supervisory authority) prompts a
  shorter or longer retention window.
- The Supabase plan changes such that storage cost is no longer a driver.

## Audit log

| Date       | Change                                                                                              |
|------------|-----------------------------------------------------------------------------------------------------|
| 2026-05-14 | Initial document drafted; codifies the 7-day window already implemented in code.                    |
