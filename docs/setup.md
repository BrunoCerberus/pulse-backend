# Setup Guide

End-to-end setup for a fresh Pulse Backend deployment: Supabase project,
database migrations, API keys, GitHub Actions, and Edge Functions.

For the repository layout and local-dev workflow, see
[development.md](development.md). For the CI/CD and deploy pipeline details, see
[ci-cd.md](ci-cd.md).

## 1. Create Supabase Project

1. Go to [supabase.com](https://supabase.com) and create a free account
2. Create a new project (remember your database password)
3. Wait for the project to initialize (~2 minutes)

## 2. Run Database Migrations

In the Supabase Dashboard, go to **SQL Editor** and run the migrations in order.
(In CI/CD these are applied automatically by `supabase db push` — see
[ci-cd.md](ci-cd.md).)

- `supabase/migrations/001_initial_schema.sql` - Core schema
- `supabase/migrations/002_add_media_support.sql` - Podcast/video support
- `supabase/migrations/003_add_podcast_video_sources.sql` - Curated sources
- `supabase/migrations/004_update_articles_with_source_view.sql` - Expose media fields in API
- `supabase/migrations/005_fix_security_issues.sql` - Harden RLS, view, and function security
- `supabase/migrations/006_add_composite_indexes.sql` - Composite indexes for query performance
- `supabase/migrations/007_add_language_support.sql` - Language column on sources & articles
- `supabase/migrations/008_add_pt_es_sources.sql` - Portuguese & Spanish RSS sources
- `supabase/migrations/009_add_more_pt_es_sources.sql` - More PT & ES sources
- `supabase/migrations/010_add_pt_es_podcasts_videos.sql` - PT & ES podcasts, videos, politics
- `supabase/migrations/011_revoke_cleanup_from_anon.sql` - Restrict cleanup function to service role only
- `supabase/migrations/012_add_content_to_search_vector.sql` - Include content in full-text search
- `supabase/migrations/013_drop_fetch_interval_minutes.sql` - Remove unused column
- `supabase/migrations/014_add_batch_image_update_rpc.sql` - Batch image update RPC
- `supabase/migrations/015_add_fetch_interval_hours.sql` - Adaptive fetch frequency
- `supabase/migrations/016_denormalize_articles.sql` - Denormalize source/category into articles
- `supabase/migrations/017_backfill_denormalized_articles.sql` - Backfill denormalized columns
- `supabase/migrations/018_add_backfill_tracking.sql` - Attempt counters + cooldown RPC for image/content backfills
- `supabase/migrations/019_add_source_fetch_state_columns.sql` - `etag`, `last_modified`, `consecutive_failures`, `circuit_open_until` on sources
- `supabase/migrations/020_add_source_health_infra.sql` - `batch_update_source_fetch_state` RPC + `source_health` view
- `supabase/migrations/021_batch_cleanup_old_articles.sql` - Batch `cleanup_old_articles` + per-function `statement_timeout` to avoid 57014 timeouts
- `supabase/migrations/022_add_db_size_rpc.sql` - `get_db_size_bytes` RPC for DB-size watchdog
- `supabase/migrations/023_inactivate_dead_sources.sql` - Data cleanup: flip `is_active=false` on long-dead/never-produced sources
- `supabase/migrations/024_strip_content_from_search_vector.sql` - Drop `content` from `search_vector` to shrink the GIN index
- `supabase/migrations/025_drop_unused_indexes.sql` - Drop indexes with `idx_scan=0` to cut write amplification
- `supabase/migrations/026_add_batch_content_update_rpc.sql` - `batch_update_article_content` RPC for batched content backfill
- `supabase/migrations/027_security_hardening.sql` - Multi-finding hardening: `search_articles` explicit projection + 200-char input cap + 3s statement_timeout; SECURITY DEFINER funcs rebuilt with `search_path = ''` and in-function role check; column-level GRANT on `articles` (anon loses `url_hash` + backfill state); `articles_with_source` recreated with explicit projection; `source_health` + `get_db_size_bytes` revoked from anon (Edge Function uses service role internally); defence-in-depth REVOKE on `fetch_logs`
- `supabase/migrations/028_search_articles_explicit_casts.sql` - Hotfix consolidation: replace `pg_catalog.least` with bare `LEAST` + add `::TEXT` casts on the VARCHAR(N) columns in `search_articles` RETURNS TABLE
- `supabase/migrations/029_compress_articles_content_lz4.sql` - Switch `articles.content` TOAST compression from `pglz` to `lz4` (Supabase PG build supports `--with-lz4`); affects new writes only — existing rows rewrite naturally over the 7-day cleanup cycle, no `VACUUM FULL` (free-tier has no maintenance window)
- `supabase/migrations/030_add_source_max_content_length.sql` - Optional per-source content length cap (`sources.max_content_length INT`, default NULL = use global). Worker clamps to `MIN(this, global maxContentLen)` at both the initial-parse site and the content backfill site — a misconfigured large value can't escape the global ceiling.
- `supabase/migrations/031_prune_old_image_urls_rpc.sql` - `prune_old_image_urls(days_to_keep)` SECURITY DEFINER RPC: batched (5000/loop) NULL of `image_url` + `thumbnail_url` on articles older than `IMAGE_PRUNE_DAYS` (default 3). `IS NOT NULL` guard skips already-pruned rows. Called as a non-fatal third step in `runCleanup`; `GetArticlesNeedingOGImage` gets a matching age filter so the backfill workflow won't re-fetch what was just nulled. Uses `request.jwt.claims->>'role'` caller gate (CURRENT_USER is dead inside SECURITY DEFINER; SESSION_USER is always `authenticator` for PostgREST).
- `supabase/migrations/032_prune_old_content_rpc.sql` - `prune_old_content(days_to_keep)` SECURITY DEFINER RPC: same shape as 031, nulls `articles.content` past `CONTENT_PRUNE_DAYS` (default 2). iOS detail view falls back to summary via `SupabaseModels.swift`'s `descriptionAndContent` mapper.
- `supabase/migrations/033_fix_security_definer_caller_gate.sql` - Replaces the dead `CURRENT_USER` caller check in all five SECURITY DEFINER write functions from migration 027 (`batch_update_article_images`, `batch_update_article_content`, `bump_backfill_attempts`, `batch_update_source_fetch_state`, `cleanup_old_articles`) with the working JWT-claim pattern from migrations 031/032. `CURRENT_USER` resolves to the definer (postgres) inside SECURITY DEFINER, so the check was always passing regardless of caller. Defence-in-depth against a future GRANT regression; GRANT was and remains the actual security boundary.
- `supabase/migrations/034_restrict_sources_columns.sql` - Closes a gap migration 027 left open: the `sources` BASE TABLE still had Supabase's default full-column anon SELECT, so anon could read operational/circuit-breaker columns (`feed_url`, `etag`, `consecutive_failures`, `circuit_open_until`, etc.) directly via PostgREST even though `source_health` and the `api-sources` Edge Function already restricted them. REVOKEs table-level SELECT from anon/authenticated and re-GRANTs only the public column set (`id, name, slug, website_url, logo_url, category_id, language, is_active`); `service_role` keeps full access.
- `supabase/migrations/035_restrict_sources_categories_writes.sql` - Defence-in-depth write REVOKE on `sources` + `categories`: writes were already blocked by RLS (no permissive write policy since migration 005), but unlike `articles`/`fetch_logs` these two tables lacked the belt-and-suspenders table-level write REVOKE. REVOKEs `INSERT/UPDATE/DELETE` from `PUBLIC, anon, authenticated` so an RLS-disable regression can't hand anon write access; `service_role` grant is re-asserted explicitly. Asserted by `security_invariants.sql` INVARIANT 9.

This creates:
- `categories` table with 10 categories (including Podcasts & Videos)
- `sources` table with 136 feeds (articles, podcasts, YouTube channels) — see [content-sources.md](content-sources.md) for the full catalog
- `articles` table with full-text search and media fields
- `fetch_logs` table for monitoring
- Row Level Security policies
- Helper functions

See [database-schema.md](database-schema.md) for the full schema reference.

## 3. Get Your API Keys

In Supabase Dashboard → **Settings** → **API**:

| Key | Purpose | Where to Use |
|-----|---------|--------------|
| **Project URL** | API base URL | iOS app + Go worker |
| **anon (public)** | Read-only access | iOS app |
| **service_role** | Full access | Go worker only (keep secret!) |

## 4. Set Up GitHub Repository

```bash
cd ~/pulse-backend
git init
git add .
git commit -m "Initial commit: Pulse backend"

# Create repo on GitHub, then:
git remote add origin git@github.com:YOUR_USERNAME/pulse-backend.git
git push -u origin master
```

## 5. Configure GitHub Secrets

Two scopes:

**Repo secrets** (Settings → Secrets and variables → Actions):

| Secret Name | Used by |
|-------------|---------|
| `SUPABASE_URL` | `fetch-rss.yml`, `cleanup.yml`, `backfill.yml`, `watchdog.yml` |
| `SUPABASE_SERVICE_ROLE_KEY` | `fetch-rss.yml`, `cleanup.yml`, `backfill.yml` (worker writes). `watchdog.yml` does **not** need it. |

**`production` Environment secrets** (Settings → Environments → New environment "production"):

| Secret Name | Used by |
|-------------|---------|
| `SUPABASE_ACCESS_TOKEN` | `deploy.yml` |
| `SUPABASE_PROJECT_REF` | `deploy.yml` |
| `SUPABASE_DB_PASSWORD` | `deploy.yml` (`supabase db push`; from Supabase dashboard → Project Settings → Database). If absent, the migration step no-ops with a notice and functions still deploy. |

Configure the `production` Environment with **required reviewers** (yourself) and
**deployment branches: master only**. This gates every deploy on a human approval
click in the Actions UI and scopes the deploy secrets so they can't be
exfiltrated from unrelated workflows. `deploy.yml` also reads the repo-scope
`SUPABASE_URL` for its post-deploy api-health smoke test.

## 6. Enable GitHub Actions

The workflows are already configured (full table in [ci-cd.md](ci-cd.md)):

- `fetch-rss.yml` - Runs every 2 hours
- `cleanup.yml` - Runs daily at 3 AM UTC
- `backfill.yml` - og:image + content backfill daily at 04:30 UTC (manual `workflow_dispatch` accepts `kind: both|images|content`)
- `watchdog.yml` - Source-health check every 6 hours (fails the job + emails when circuit/stale/high-failure/DB-quota thresholds breach)
- `migrations-ci.yml` - On PR/push touching `supabase/migrations/**`, `supabase/config.toml`, or `supabase/tests/**`: boots the local Supabase stack, applies all migrations from scratch, `supabase db lint`, then runs the SQL security-invariant assertions
- `lint-meta.yml` - `actionlint` (+ shellcheck on run-blocks) over all workflows on PR/push

Go to the **Actions** tab and enable workflows if prompted.

## 7. Deploy Edge Functions

Two paths:

**Manual (first deploy):**

```bash
# Install Supabase CLI
brew install supabase/tap/supabase

# Login and link project
supabase login
supabase link --project-ref YOUR_PROJECT_REF

# Deploy all functions
supabase functions deploy
```

**Automated (after the `production` Environment is set up):** any merge to
`master` touching `supabase/migrations/**`, `supabase/functions/**`, or
`supabase/config.toml` queues a deploy in `deploy.yml`. The job pauses on the
"Waiting" step until a maintainer clicks **Approve and deploy** in the Actions
UI, then runs three ordered steps under `set -e`: (1) **apply migrations** via
`supabase db push` (no-ops with a notice if `SUPABASE_DB_PASSWORD` is unset, so
functions still ship); (2) **deploy Edge Functions** via
`supabase functions deploy`; (3) **api-health smoke test** that curls
`${SUPABASE_URL}/functions/v1/api-health` with retries and expects
`{"status":"ok"}`. A failed migration aborts before functions deploy.

The Edge Functions provide a caching layer for the iOS app with Cache-Control
headers. They also enforce request guards (column whitelist, `limit` clamp, `q`
length cap, URL length cap, oversized-value drop) documented in
[api-reference.md](api-reference.md).

## 8. Test the Worker

Trigger a manual run:
1. Go to **Actions** → **Fetch RSS Feeds**
2. Click **Run workflow**
3. Check the logs to see articles being fetched

## 9. Verify in Supabase

Go to **Table Editor** → **articles** to see the fetched articles.

---

**Next steps:** [Local development & repo layout](development.md) ·
[API reference](api-reference.md) · [iOS integration](ios-integration.md) ·
[Operations runbook](operations-runbook.md)
