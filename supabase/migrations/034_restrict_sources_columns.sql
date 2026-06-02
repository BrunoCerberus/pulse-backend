-- =============================================================================
-- 034_restrict_sources_columns.sql
-- =============================================================================
-- C1 (from the 2026-06 security discovery sweep): restrict anon/authenticated
-- column access on public.sources.
--
-- Migration 027 hardened `articles`, `fetch_logs`, `source_health`, and
-- `get_db_size_bytes`, but left the `sources` BASE TABLE with Supabase's default
-- full-column anon SELECT. The RLS policy from migration 001 ("Public read
-- access for sources" USING (is_active = true)) is a ROW filter only — it does
-- not restrict columns. So an anonymous caller holding the public app key can
-- read operational / reconnaissance columns directly via PostgREST
--   (feed_url, last_fetched_at, fetch_interval_hours, etag, last_modified,
--    consecutive_failures, circuit_open_until, max_content_length)
-- even though `source_health` was revoked from anon in 027 and the api-sources
-- Edge Function only selects the public set. The circuit-breaker / fetch-state
-- columns are exactly what 027's H8 revoke meant to keep from anon; this closes
-- the base-table path it missed.
--
-- Fix (mirrors the articles column-grant pattern in migration 027 H6): REVOKE
-- the table-level SELECT from anon/authenticated and re-GRANT only the public
-- column set that the api-sources Edge Function serves
--   (defaultSelect = id,name,slug,website_url,logo_url,category_id,language,is_active).
-- service_role is untouched and keeps full access via its default grants — the
-- worker reads every column. The RLS row filter is unchanged.
-- =============================================================================

REVOKE SELECT ON public.sources FROM anon, authenticated;

-- Exactly the columns api-sources exposes (defaultSelect) and filters/orders on
-- (id, slug, category_id, language, is_active; name/slug/language ordering).
GRANT SELECT (
    id,
    name,
    slug,
    website_url,
    logo_url,
    category_id,
    language,
    is_active
) ON public.sources TO anon, authenticated;
-- service_role retains full SELECT via its default grants.
