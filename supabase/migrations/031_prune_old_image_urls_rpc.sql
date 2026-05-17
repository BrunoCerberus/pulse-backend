-- Migration 031: per-day prune of image_url / thumbnail_url on stale articles
--
-- Articles older than IMAGE_PRUNE_DAYS (worker default 3) rarely surface in
-- the iOS list view; their image_url / thumbnail_url are long strings
-- (~120-200 chars each) and represent meaningful TOAST footprint. This RPC
-- nulls them out in 5000-row batches so the daily cleanup job can reclaim
-- the space gradually without statement-timeout pressure.
--
-- The DELETE-driven cleanup_old_articles (migration 021) still removes the
-- entire row at day 7; this prune just shrinks rows in the 3-7d window.
-- Storage win shows up as slope reduction, not an immediate `pg_database_
-- size` drop — PostgreSQL UPDATEs leave dead tuples until the row is fully
-- deleted at retention.
--
-- Hardening (matches migration 027 pattern):
--   * SECURITY DEFINER with `search_path = ''` so a future REVOKE typo or
--     signature-overload gap can't expose write paths.
--   * In-function role check rejects everything outside service_role /
--     postgres so anon/authenticated can't invoke even if a future REVOKE
--     regresses.
--   * statement_timeout = '5min' caps the worst-case lock window.
--   * Batched LIMIT 5000 mirrors cleanup_old_articles to keep per-batch
--     lock contention low.
--   * `AND (image_url IS NOT NULL OR thumbnail_url IS NOT NULL)` guard
--     skips already-pruned rows so repeated runs don't churn dead tuples
--     for zero benefit.

CREATE OR REPLACE FUNCTION public.prune_old_image_urls(days_to_keep INTEGER)
RETURNS BIGINT
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = ''
SET statement_timeout = '5min'
AS $$
DECLARE
    cutoff TIMESTAMPTZ := pg_catalog.now() - (days_to_keep || ' days')::INTERVAL;
    batch_size CONSTANT INT := 5000;
    rows_updated BIGINT;
    total_updated BIGINT := 0;
BEGIN
    -- In-function role check (matches migration 027 pattern — allow postgres
    -- for local admin / migration runs, block anon/authenticated).
    IF CURRENT_USER NOT IN ('service_role', 'postgres') THEN
        RAISE EXCEPTION 'access denied for user %', CURRENT_USER;
    END IF;

    LOOP
        WITH victims AS (
            SELECT id
            FROM public.articles
            WHERE created_at < cutoff
              AND (image_url IS NOT NULL OR thumbnail_url IS NOT NULL)
            LIMIT batch_size
        )
        UPDATE public.articles a
        SET image_url = NULL,
            thumbnail_url = NULL
        FROM victims
        WHERE a.id = victims.id;

        GET DIAGNOSTICS rows_updated = ROW_COUNT;
        EXIT WHEN rows_updated = 0;
        total_updated := total_updated + rows_updated;
    END LOOP;

    RETURN total_updated;
END;
$$;

REVOKE ALL ON FUNCTION public.prune_old_image_urls(INTEGER) FROM PUBLIC, anon, authenticated;
GRANT EXECUTE ON FUNCTION public.prune_old_image_urls(INTEGER) TO service_role;

COMMENT ON FUNCTION public.prune_old_image_urls(INTEGER) IS
    'Batched NULL-out of image_url + thumbnail_url on articles older than days_to_keep. service_role only. IS NOT NULL guard prevents repeat runs from churning dead tuples.';
