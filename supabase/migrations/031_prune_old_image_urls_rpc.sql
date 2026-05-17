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
-- Hardening:
--   * SECURITY DEFINER with `search_path = ''` so a future schema-search-
--     path attack can't redirect built-in references.
--   * Defence-in-depth caller check below (see WHY).
--   * statement_timeout = '5min' caps the worst-case runtime per call.
--   * Batched LIMIT 5000 keeps per-statement plan size and work_mem
--     footprint small. NOTE: this LOOP runs inside a single transaction
--     (PL/pgSQL function bodies always do), so iterations do NOT release
--     row/page locks between batches — locks are held for the full RPC
--     duration. The batching benefit here is planner/memory pressure,
--     plus the `IS NOT NULL` guard letting the loop self-exit cleanly.
--   * `AND (image_url IS NOT NULL OR thumbnail_url IS NOT NULL)` guard
--     skips already-pruned rows so repeated runs don't churn dead tuples
--     for zero benefit.
--
-- WHY the caller check uses request.jwt.claims and not CURRENT_USER:
-- Inside a SECURITY DEFINER function, `CURRENT_USER` resolves to the
-- function owner (postgres), NOT the caller — so a check like
-- `CURRENT_USER NOT IN ('service_role', 'postgres')` is dead code; it
-- always passes regardless of who invoked the RPC. `SESSION_USER`
-- doesn't help either: for any PostgREST request it returns
-- `authenticator` (the role PostgREST connects as), masking whether
-- the caller's JWT asserted anon, authenticated, or service_role.
--
-- The actually-load-bearing security is the GRANT below (REVOKE from
-- PUBLIC/anon/authenticated + GRANT only to service_role). The check
-- below is belt-and-suspenders: if a future GRANT regression (or a
-- typo in a follow-up migration) accidentally widens execute rights,
-- it still blocks PostgREST callers whose JWT role isn't service_role,
-- and it still allows direct postgres connections (migrations, the SQL
-- editor) where there is no JWT.

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
    jwt_role TEXT := NULLIF(pg_catalog.current_setting('request.jwt.claims', true), '');
    rows_updated BIGINT;
    total_updated BIGINT := 0;
BEGIN
    -- Defence-in-depth caller check. See header comment for the WHY.
    -- jwt_role is the raw JSON string ("" when no JWT, e.g. direct postgres
    -- connection); parsing it via ->>'role' yields the role claim or NULL.
    -- SESSION_USER / CURRENT_USER are SQL keywords (not pg_catalog functions)
    -- so they don't need (and don't accept) schema qualification even under
    -- `search_path = ''`.
    IF NOT (
        (jwt_role IS NOT NULL AND (jwt_role::jsonb ->> 'role') = 'service_role')
        OR SESSION_USER = 'postgres'
    ) THEN
        RAISE EXCEPTION 'access denied for session_user=%, jwt_role=%',
            SESSION_USER, COALESCE(jwt_role, '<unset>');
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
