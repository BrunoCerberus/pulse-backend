-- Migration 036: language filter + offset for search_articles
--
-- Two gaps in the search path, both visible in the iOS app:
--
--   1) `search_articles` matched across every language in the corpus. The
--      app has a user-selected content language (Settings → Language) and
--      filters every other endpoint with `language=eq.<code>`, but search
--      had no way to pass it, so results came back mixed and looked like
--      they were keyed off the device locale rather than the app setting.
--   2) The function had no `offset`, so /api-search paginated by re-running
--      the same top-N query. The client's "load more" fetched page 1 again
--      and deduplicated it away to nothing.
--
-- `search_language` defaults to NULL = match all languages, so any caller
-- still on the two-argument call site keeps today's behaviour. Values are
-- ISO 639-1 codes matching `articles.language`; the tsquery config stays
-- 'english' because `articles.search_vector` is a GENERATED column built
-- with that config (migration 001) — querying it with another config would
-- simply stop matching. Cross-language stemming is a separate change that
-- needs the generated column rebuilt.
--
-- Replaces the two-argument signature rather than overloading it: PostgREST
-- resolves RPCs by argument name, and two candidates reachable from the same
-- named-argument set is an ambiguity waiting to 300.

DROP FUNCTION IF EXISTS public.search_articles(TEXT, INT);

CREATE OR REPLACE FUNCTION public.search_articles(
    search_query TEXT,
    result_limit INT DEFAULT 20,
    search_language TEXT DEFAULT NULL,
    result_offset INT DEFAULT 0
)
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
      AND (search_language IS NULL OR a.language = search_language)
    ORDER BY pg_catalog.ts_rank(
                 a.search_vector,
                 pg_catalog.plainto_tsquery('english', search_query)
             ) DESC,
             a.published_at DESC
    LIMIT LEAST(result_limit, 100)
    OFFSET GREATEST(COALESCE(result_offset, 0), 0);
END;
$$ LANGUAGE plpgsql
   STABLE
   SECURITY DEFINER
   SET search_path = ''
   SET statement_timeout = '3s';

REVOKE ALL ON FUNCTION public.search_articles(TEXT, INT, TEXT, INT) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.search_articles(TEXT, INT, TEXT, INT)
    TO anon, authenticated, service_role;
