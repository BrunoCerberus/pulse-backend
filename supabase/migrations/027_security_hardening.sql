-- Migration 027: Security hardening (multi-surface)
--
-- Closes findings from the 2026-05-13 security audit:
--
--   * C1  — search_articles returned SETOF articles, exposing
--           image_backfill_attempts / search_vector / etc. to anon.
--   * C5  — migrations 014/018/020/026 used SET search_path = public on
--           SECURITY DEFINER functions. pg_temp is always implicitly
--           prepended; an attacker who can create objects in pg_temp could
--           shadow built-ins (count, jsonb_to_recordset, now, …) when the
--           function runs as definer.
--   * H6  — `Public read access for articles USING (true)` plus a broad
--           SELECT on `articles` exposed every column to anon (image
--           backfill state, search_vector, url_hash, …).
--   * H7  — get_db_size_bytes was anon-callable, leaking ~real-time DB
--           growth (useful as a "time-your-quota-DoS" signal).
--   * H8  — source_health view was anon-readable; a future view-column
--           addition would silently leak operational data.
--   * H14 — cleanup_old_articles relied on a single REVOKE for safety;
--           a future overload signature could regress it.
--   * H16/M-1 — search RPC had no length cap or statement timeout; a
--           large `q` would build an arbitrary-size tsquery tree.
--
-- Behavioral notes:
--   * api-source-health Edge Function is rewritten in this PR to call
--     upstream with the service-role key (not anon), so revoking
--     source_health + get_db_size_bytes from anon does not break it.
--   * articles_with_source is recreated to project only the public set of
--     columns. iOS reads of the view continue to work; direct anon SELECT
--     on `articles` is now column-level (no backfill / search-vector leak).
--   * search_articles becomes SECURITY DEFINER so it bypasses the
--     column-level grants on `articles` while still only returning the
--     explicit safe projection. Adds a 200-char input cap and a 3s
--     statement_timeout to bound DoS surface.


-- =============================================================================
-- search_articles: explicit projection + length cap + timeout
-- =============================================================================

DROP FUNCTION IF EXISTS public.search_articles(TEXT, INT);

CREATE FUNCTION public.search_articles(search_query TEXT, result_limit INT DEFAULT 20)
RETURNS TABLE (
    id UUID,
    title TEXT,
    summary TEXT,
    content TEXT,
    url TEXT,
    image_url TEXT,
    thumbnail_url TEXT,
    author TEXT,
    published_at TIMESTAMPTZ,
    language VARCHAR(5),
    source_id UUID,
    category_id UUID,
    source_name TEXT,
    source_slug TEXT,
    category_name TEXT,
    category_slug TEXT,
    media_type TEXT,
    media_url TEXT,
    media_duration INT,
    media_mime_type TEXT
) AS $$
BEGIN
    -- Reject empty / oversized queries before they cost a tsquery build.
    IF search_query IS NULL
       OR pg_catalog.length(search_query) = 0
       OR pg_catalog.length(search_query) > 200 THEN
        RETURN;
    END IF;

    RETURN QUERY
    SELECT a.id, a.title, a.summary, a.content, a.url, a.image_url,
           a.thumbnail_url, a.author, a.published_at, a.language,
           a.source_id, a.category_id, a.source_name, a.source_slug,
           a.category_name, a.category_slug, a.media_type, a.media_url,
           a.media_duration, a.media_mime_type
    FROM public.articles a
    WHERE a.search_vector @@ pg_catalog.plainto_tsquery('english', search_query)
    ORDER BY pg_catalog.ts_rank(
                 a.search_vector,
                 pg_catalog.plainto_tsquery('english', search_query)
             ) DESC,
             a.published_at DESC
    LIMIT pg_catalog.least(result_limit, 100);
END;
$$ LANGUAGE plpgsql
   STABLE
   SECURITY DEFINER         -- bypass anon's column-level grants on articles
   SET search_path = ''
   SET statement_timeout = '3s';

REVOKE ALL ON FUNCTION public.search_articles(TEXT, INT) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.search_articles(TEXT, INT) TO anon, authenticated, service_role;


-- =============================================================================
-- C5: rebuild SECURITY DEFINER functions with hardened search_path
-- =============================================================================

-- batch_update_article_images: was migration 014
CREATE OR REPLACE FUNCTION public.batch_update_article_images(updates JSONB)
RETURNS INTEGER AS $$
DECLARE updated_count INTEGER;
BEGIN
    -- Defence-in-depth: refuse callers outside service_role / postgres so a
    -- future REVOKE typo or signature-overload gap can't expose write paths.
    IF CURRENT_USER NOT IN ('service_role', 'postgres') THEN
        RAISE EXCEPTION 'access denied for user %', CURRENT_USER;
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


-- batch_update_article_content: was migration 026
CREATE OR REPLACE FUNCTION public.batch_update_article_content(updates JSONB)
RETURNS INTEGER AS $$
DECLARE updated_count INTEGER;
BEGIN
    IF CURRENT_USER NOT IN ('service_role', 'postgres') THEN
        RAISE EXCEPTION 'access denied for user %', CURRENT_USER;
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


-- bump_backfill_attempts: was migration 018. Adds array-length cap.
CREATE OR REPLACE FUNCTION public.bump_backfill_attempts(url_hashes TEXT[], kind TEXT)
RETURNS INTEGER AS $$
DECLARE updated_count INTEGER;
BEGIN
    IF CURRENT_USER NOT IN ('service_role', 'postgres') THEN
        RAISE EXCEPTION 'access denied for user %', CURRENT_USER;
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


-- batch_update_source_fetch_state: was migration 020
CREATE OR REPLACE FUNCTION public.batch_update_source_fetch_state(updates JSONB)
RETURNS INTEGER AS $$
DECLARE updated_count INTEGER;
BEGIN
    IF CURRENT_USER NOT IN ('service_role', 'postgres') THEN
        RAISE EXCEPTION 'access denied for user %', CURRENT_USER;
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
-- H14: cleanup_old_articles in-function role check
-- =============================================================================

CREATE OR REPLACE FUNCTION public.cleanup_old_articles(days_to_keep INT DEFAULT 30)
RETURNS INT AS $$
DECLARE
    deleted_count INT := 0;
    batch_count INT;
    cutoff TIMESTAMPTZ := pg_catalog.now() - (days_to_keep || ' days')::INTERVAL;
BEGIN
    IF CURRENT_USER NOT IN ('service_role', 'postgres') THEN
        RAISE EXCEPTION 'access denied for user %', CURRENT_USER;
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


-- =============================================================================
-- H7: revoke get_db_size_bytes from anon/authenticated.
-- =============================================================================
-- The Edge Function api-source-health now authenticates upstream with the
-- service-role key (see C2 fix), so it can call this even after the revoke.

REVOKE EXECUTE ON FUNCTION public.get_db_size_bytes() FROM anon, authenticated;
-- service_role grant from migration 022 is preserved.


-- =============================================================================
-- H8: revoke source_health from anon/authenticated.
-- =============================================================================

REVOKE SELECT ON public.source_health FROM anon, authenticated;
GRANT SELECT ON public.source_health TO service_role;


-- =============================================================================
-- H6: column-level SELECT on articles + recreated view projecting safe cols
-- =============================================================================

-- Drop the old view first so the underlying privilege change below doesn't
-- error out citing a dependency.
DROP VIEW IF EXISTS public.articles_with_source;

REVOKE SELECT ON public.articles FROM anon, authenticated;
-- `search_vector` is exposed because the iOS app's search uses PostgREST's
-- `.textSearch("search_vector", ...)` directly against `articles_with_source`.
-- Its content is derived from title/summary which are already public, so
-- granting it adds no privacy surface; just makes it queryable.
GRANT SELECT (
    id, title, summary, content, url, image_url, thumbnail_url, author,
    published_at, created_at, language, source_id, category_id,
    source_name, source_slug, category_name, category_slug,
    media_type, media_url, media_duration, media_mime_type,
    search_vector
) ON public.articles TO anon, authenticated;
-- service_role retains full SELECT via its default grants.

CREATE VIEW public.articles_with_source
WITH (security_invoker = on) AS
SELECT
    id, title, summary, content, url, image_url, thumbnail_url, author,
    published_at, created_at, language, source_id, category_id,
    source_name, source_slug, category_name, category_slug,
    media_type, media_url, media_duration, media_mime_type,
    search_vector
FROM public.articles;

GRANT SELECT ON public.articles_with_source TO anon, authenticated, service_role;


-- =============================================================================
-- Defence-in-depth: revoke direct access on fetch_logs from anon/authenticated.
-- =============================================================================
-- Migration 005 already dropped the broad "Service role insert for fetch_logs"
-- policy. RLS is enabled with no anon SELECT policy. Adding REVOKE belt-and-
-- suspenders so a future erroneous policy can't accidentally expose error
-- payloads which sometimes contain feed URLs / partial response bodies.

REVOKE ALL ON public.fetch_logs FROM anon, authenticated;
GRANT ALL ON public.fetch_logs TO service_role;
