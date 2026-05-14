-- Migration 028: hotfix search_articles return-type wiring
--
-- This consolidates three same-day production hotfixes that landed during
-- the security-hardening deploy. Migration 027 introduced search_articles
-- with `RETURNS TABLE(..., text, ...)` and a body qualified via pg_catalog
-- for search_path = '' safety. Two issues surfaced at first call:
--
--   1) `pg_catalog.least(...)` — LEAST is a SQL construct, not a
--      catalog-registered function. The qualified call failed with
--      42883 ("function pg_catalog.least(integer, integer) does not exist").
--   2) `RETURNS TABLE` column types are strict: char varying(N) does not
--      implicitly coerce to text. Several articles columns are VARCHAR(N)
--      (author, source_name, source_slug, category_name, category_slug,
--      media_type, media_mime_type), each returning 42804 in turn until
--      every projection got an explicit ::TEXT cast.
--
-- Both issues are fixed below. Casting in the SELECT (rather than aligning
-- the signature to today's column types) keeps the function resilient to a
-- future ALTER TABLE … TYPE TEXT migration without a third hotfix.
--
-- This migration was applied to production live as three separate files
-- (028, 029, 030) during the deploy. Those entries were marked reverted in
-- `supabase_migrations.schema_migrations` via `supabase migration repair`
-- and replaced with this single file to keep the on-disk history readable.
-- `CREATE OR REPLACE FUNCTION` + idempotent REVOKE/GRANT make the
-- re-application a no-op for the current DB state.

CREATE OR REPLACE FUNCTION public.search_articles(search_query TEXT, result_limit INT DEFAULT 20)
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
    IF search_query IS NULL
       OR pg_catalog.length(search_query) = 0
       OR pg_catalog.length(search_query) > 200 THEN
        RETURN;
    END IF;

    RETURN QUERY
    SELECT a.id,
           a.title,
           a.summary,
           a.content,
           a.url,
           a.image_url,
           a.thumbnail_url,
           a.author::TEXT,
           a.published_at,
           a.language,
           a.source_id,
           a.category_id,
           a.source_name::TEXT,
           a.source_slug::TEXT,
           a.category_name::TEXT,
           a.category_slug::TEXT,
           a.media_type::TEXT,
           a.media_url,
           a.media_duration,
           a.media_mime_type::TEXT
    FROM public.articles a
    WHERE a.search_vector @@ pg_catalog.plainto_tsquery('english', search_query)
    ORDER BY pg_catalog.ts_rank(
                 a.search_vector,
                 pg_catalog.plainto_tsquery('english', search_query)
             ) DESC,
             a.published_at DESC
    LIMIT LEAST(result_limit, 100);
END;
$$ LANGUAGE plpgsql
   STABLE
   SECURITY DEFINER
   SET search_path = ''
   SET statement_timeout = '3s';

REVOKE ALL ON FUNCTION public.search_articles(TEXT, INT) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.search_articles(TEXT, INT) TO anon, authenticated, service_role;
