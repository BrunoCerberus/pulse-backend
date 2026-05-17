-- Migration 032: per-day prune of articles.content on stale articles
--
-- The largest single-column cost in the articles table — full extracted body
-- text capped at 200K runes by the parser, LZ4-compressed since migration
-- 029. The iOS list view + search only need title + summary; content is
-- only surfaced by the article-detail view, which iOS hits via the
-- articles_with_source view (docs/ios-integration.md). This RPC nulls
-- content on articles older than CONTENT_PRUNE_DAYS (worker default 2)
-- while metadata stays until the day-7 row deletion.
--
-- DESTRUCTIVE to the iOS article-detail view for articles 2-7d old.
-- iOS MUST handle NULL content gracefully ("View on source" / summary
-- fallback) BEFORE this migration is applied. Backend tests can only
-- prove content goes NULL; the user-visible fallback lives in the iOS
-- app and is verified on-device.
--
-- Storage win is gradual: PostgreSQL UPDATEs leave dead tuples until the
-- row is fully deleted at retention. Expect slope reduction across one
-- 7-day cleanup_old_articles cycle, not an immediate pg_database_size
-- drop. Largest steady-state win of the data-reduction series.
--
-- Hardening: SECURITY DEFINER + `search_path = ''` + JWT-claim caller
-- gate (see WHY block below) + statement_timeout + batched LIMIT 5000
-- (NOTE: PL/pgSQL function bodies run in a single transaction, so the
-- LOOP does NOT release row/page locks between iterations — locks are
-- held for the full RPC duration; the batching benefit is planner /
-- work_mem footprint and clean self-exit via the IS NOT NULL guard).
-- The `AND content IS NOT NULL` guard skips already-pruned rows so
-- repeated runs don't churn dead tuples for zero benefit.
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

CREATE OR REPLACE FUNCTION public.prune_old_content(days_to_keep INTEGER)
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
    -- Defence-in-depth caller gate. SESSION_USER / CURRENT_USER are SQL
    -- keywords (not pg_catalog functions) so they don't accept schema
    -- qualification even under `search_path = ''`. See header for the WHY.
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
              AND content IS NOT NULL
            LIMIT batch_size
        )
        UPDATE public.articles a
        SET content = NULL
        FROM victims
        WHERE a.id = victims.id;

        GET DIAGNOSTICS rows_updated = ROW_COUNT;
        EXIT WHEN rows_updated = 0;
        total_updated := total_updated + rows_updated;
    END LOOP;

    RETURN total_updated;
END;
$$;

REVOKE ALL ON FUNCTION public.prune_old_content(INTEGER) FROM PUBLIC, anon, authenticated;
GRANT EXECUTE ON FUNCTION public.prune_old_content(INTEGER) TO service_role;

COMMENT ON FUNCTION public.prune_old_content(INTEGER) IS
    'Batched NULL-out of content on articles older than days_to_keep. service_role only. IS NOT NULL guard prevents repeat runs from churning dead tuples. DESTRUCTIVE to iOS article-detail for 2-7d articles — iOS must handle NULL content before this is invoked.';
