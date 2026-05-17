-- Migration 033: replace broken CURRENT_USER caller gate in all SECURITY
-- DEFINER write functions from migration 027 with the working JWT-claim
-- pattern verified in migrations 031 + 032.
--
-- WHY this is needed:
-- Migration 027 introduced an in-function caller check across five write
-- functions:
--   IF CURRENT_USER NOT IN ('service_role', 'postgres') THEN
--     RAISE EXCEPTION 'access denied for user %', CURRENT_USER;
--   END IF;
--
-- That check is dead code. Inside a SECURITY DEFINER function, PostgreSQL
-- resolves `CURRENT_USER` to the function owner (postgres), NOT the caller.
-- Empirically confirmed against production:
--
--   | Caller path             | CURRENT_USER | SESSION_USER  | JWT role     |
--   |-------------------------|--------------|---------------|--------------|
--   | anon (PostgREST)        | postgres     | authenticator | anon         |
--   | service_role (PostgREST)| postgres     | authenticator | service_role |
--   | postgres (direct conn)  | postgres     | postgres      | (no JWT)     |
--
-- So `CURRENT_USER NOT IN ('service_role', 'postgres')` always evaluates to
-- false, regardless of who invoked the function. The check never fires.
-- SESSION_USER doesn't help either: PostgREST connects to PG as
-- `authenticator` and then SET ROLEs based on the JWT, so SESSION_USER =
-- 'authenticator' for every PostgREST caller, masking anon vs service_role.
--
-- The actually-load-bearing security in these five functions IS and HAS
-- BEEN the GRANT: `REVOKE EXECUTE FROM PUBLIC, anon, authenticated` +
-- `GRANT EXECUTE TO service_role`. That's what blocks unauthorized callers
-- today (verified: anon gets `42501 permission denied for function ...`
-- from PostgREST before the function body even runs). The in-function
-- check was meant as belt-and-suspenders defence against a future GRANT
-- regression — e.g. a misconfigured `GRANT EXECUTE ... TO anon` typo in
-- the SQL editor — but it currently provides no such defence.
--
-- The fix below uses the same pattern from migrations 031 + 032: extract
-- the role from `request.jwt.claims` (the per-request GUC PostgREST
-- populates from the caller's JWT), and accept either:
--   * PostgREST callers whose JWT role claim is 'service_role', or
--   * Direct postgres connections (SESSION_USER = 'postgres') for
--     migrations and the SQL editor where there is no JWT.
--
-- No behaviour change for legitimate callers. CREATE OR REPLACE preserves
-- existing GRANTs but we re-issue them for idempotency.

-- =============================================================================
-- batch_update_article_images
-- =============================================================================

CREATE OR REPLACE FUNCTION public.batch_update_article_images(updates JSONB)
RETURNS INTEGER AS $$
DECLARE
    updated_count INTEGER;
    jwt_role TEXT := NULLIF(pg_catalog.current_setting('request.jwt.claims', true), '');
BEGIN
    IF NOT (
        (jwt_role IS NOT NULL AND (jwt_role::jsonb ->> 'role') = 'service_role')
        OR SESSION_USER = 'postgres'
    ) THEN
        RAISE EXCEPTION 'access denied for session_user=%, jwt_role=%',
            SESSION_USER, COALESCE(jwt_role, '<unset>');
    END IF;
    WITH updated AS (
        UPDATE public.articles a
        SET image_url = u.image_url
        FROM pg_catalog.jsonb_to_recordset(updates) AS u(url_hash TEXT, image_url TEXT)
        WHERE a.url_hash = u.url_hash
          AND (a.image_url IS NULL OR a.image_url != u.image_url)
        RETURNING 1
    )
    SELECT pg_catalog.count(*) INTO updated_count FROM updated;
    RETURN updated_count;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = '';

REVOKE ALL ON FUNCTION public.batch_update_article_images(JSONB) FROM PUBLIC, anon, authenticated;
GRANT EXECUTE ON FUNCTION public.batch_update_article_images(JSONB) TO service_role;


-- =============================================================================
-- batch_update_article_content
-- =============================================================================

CREATE OR REPLACE FUNCTION public.batch_update_article_content(updates JSONB)
RETURNS INTEGER AS $$
DECLARE
    updated_count INTEGER;
    jwt_role TEXT := NULLIF(pg_catalog.current_setting('request.jwt.claims', true), '');
BEGIN
    IF NOT (
        (jwt_role IS NOT NULL AND (jwt_role::jsonb ->> 'role') = 'service_role')
        OR SESSION_USER = 'postgres'
    ) THEN
        RAISE EXCEPTION 'access denied for session_user=%, jwt_role=%',
            SESSION_USER, COALESCE(jwt_role, '<unset>');
    END IF;
    WITH updated AS (
        UPDATE public.articles a
        SET content = u.content
        FROM pg_catalog.jsonb_to_recordset(updates) AS u(url_hash TEXT, content TEXT)
        WHERE a.url_hash = u.url_hash
          AND (a.content IS NULL OR a.content = '' OR a.content != u.content)
        RETURNING 1
    )
    SELECT pg_catalog.count(*) INTO updated_count FROM updated;
    RETURN updated_count;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = '';

REVOKE ALL ON FUNCTION public.batch_update_article_content(JSONB) FROM PUBLIC, anon, authenticated;
GRANT EXECUTE ON FUNCTION public.batch_update_article_content(JSONB) TO service_role;


-- =============================================================================
-- bump_backfill_attempts
-- =============================================================================

CREATE OR REPLACE FUNCTION public.bump_backfill_attempts(url_hashes TEXT[], kind TEXT)
RETURNS INTEGER AS $$
DECLARE
    updated_count INTEGER;
    jwt_role TEXT := NULLIF(pg_catalog.current_setting('request.jwt.claims', true), '');
BEGIN
    IF NOT (
        (jwt_role IS NOT NULL AND (jwt_role::jsonb ->> 'role') = 'service_role')
        OR SESSION_USER = 'postgres'
    ) THEN
        RAISE EXCEPTION 'access denied for session_user=%, jwt_role=%',
            SESSION_USER, COALESCE(jwt_role, '<unset>');
    END IF;
    IF pg_catalog.array_length(url_hashes, 1) > 10000 THEN
        RAISE EXCEPTION 'too many url_hashes: max 10000';
    END IF;
    IF kind = 'image' THEN
        WITH updated AS (
            UPDATE public.articles
            SET image_backfill_attempts = image_backfill_attempts + 1,
                image_backfill_last_attempt_at = pg_catalog.now()
            WHERE url_hash = ANY(url_hashes)
            RETURNING 1
        )
        SELECT pg_catalog.count(*) INTO updated_count FROM updated;
    ELSIF kind = 'content' THEN
        WITH updated AS (
            UPDATE public.articles
            SET content_backfill_attempts = content_backfill_attempts + 1,
                content_backfill_last_attempt_at = pg_catalog.now()
            WHERE url_hash = ANY(url_hashes)
            RETURNING 1
        )
        SELECT pg_catalog.count(*) INTO updated_count FROM updated;
    ELSE
        RAISE EXCEPTION 'unknown backfill kind: %', kind;
    END IF;
    RETURN updated_count;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = '';

REVOKE ALL ON FUNCTION public.bump_backfill_attempts(TEXT[], TEXT) FROM PUBLIC, anon, authenticated;
GRANT EXECUTE ON FUNCTION public.bump_backfill_attempts(TEXT[], TEXT) TO service_role;


-- =============================================================================
-- batch_update_source_fetch_state
-- =============================================================================

CREATE OR REPLACE FUNCTION public.batch_update_source_fetch_state(updates JSONB)
RETURNS INTEGER AS $$
DECLARE
    updated_count INTEGER;
    jwt_role TEXT := NULLIF(pg_catalog.current_setting('request.jwt.claims', true), '');
BEGIN
    IF NOT (
        (jwt_role IS NOT NULL AND (jwt_role::jsonb ->> 'role') = 'service_role')
        OR SESSION_USER = 'postgres'
    ) THEN
        RAISE EXCEPTION 'access denied for session_user=%, jwt_role=%',
            SESSION_USER, COALESCE(jwt_role, '<unset>');
    END IF;
    WITH updated AS (
        UPDATE public.sources s
        SET
            etag                 = u.etag,
            last_modified        = u.last_modified,
            consecutive_failures = u.consecutive_failures,
            circuit_open_until   = u.circuit_open_until,
            last_fetched_at      = COALESCE(u.last_fetched_at, s.last_fetched_at),
            updated_at           = pg_catalog.now()
        FROM pg_catalog.jsonb_to_recordset(updates) AS u(
            id                   UUID,
            etag                 TEXT,
            last_modified        TEXT,
            consecutive_failures INT,
            circuit_open_until   TIMESTAMPTZ,
            last_fetched_at      TIMESTAMPTZ
        )
        WHERE s.id = u.id
        RETURNING 1
    )
    SELECT pg_catalog.count(*) INTO updated_count FROM updated;
    RETURN updated_count;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = '';

REVOKE ALL ON FUNCTION public.batch_update_source_fetch_state(JSONB) FROM PUBLIC, anon, authenticated;
GRANT EXECUTE ON FUNCTION public.batch_update_source_fetch_state(JSONB) TO service_role;


-- =============================================================================
-- cleanup_old_articles
-- =============================================================================

CREATE OR REPLACE FUNCTION public.cleanup_old_articles(days_to_keep INT DEFAULT 30)
RETURNS INT AS $$
DECLARE
    deleted_count INT := 0;
    batch_count INT;
    cutoff TIMESTAMPTZ := pg_catalog.now() - (days_to_keep || ' days')::INTERVAL;
    jwt_role TEXT := NULLIF(pg_catalog.current_setting('request.jwt.claims', true), '');
BEGIN
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
            ORDER BY created_at
            LIMIT 5000
        )
        DELETE FROM public.articles a
        USING victims v
        WHERE a.id = v.id;

        GET DIAGNOSTICS batch_count = ROW_COUNT;
        deleted_count := deleted_count + batch_count;
        EXIT WHEN batch_count = 0;
    END LOOP;
    RETURN deleted_count;
END;
$$ LANGUAGE plpgsql
   SECURITY DEFINER
   SET search_path = ''
   SET statement_timeout = '5min';

REVOKE ALL ON FUNCTION public.cleanup_old_articles(INT) FROM PUBLIC, anon, authenticated;
GRANT EXECUTE ON FUNCTION public.cleanup_old_articles(INT) TO service_role;
