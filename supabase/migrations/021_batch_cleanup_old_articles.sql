-- Migration 021: Batch cleanup_old_articles to avoid statement_timeout
--
-- The previous single-statement DELETE could hit statement_timeout on a busy
-- articles table (observed in the daily cleanup workflow: Postgres 57014
-- "canceling statement due to statement timeout"). A single bulk DELETE also
-- holds locks on every index for the duration of the run, blocking the worker's
-- inserts.
--
-- This rewrite:
--   * Deletes in batches of 5,000 rows, ordered by created_at so each batch
--     touches a localized range of heap + index pages.
--   * Pins a per-function statement_timeout of 5 minutes, overriding whatever
--     the caller role (service_role / postgrest) has configured, so a long run
--     completes instead of being killed mid-way.

CREATE OR REPLACE FUNCTION public.cleanup_old_articles(days_to_keep INT DEFAULT 30)
RETURNS INT AS $$
DECLARE
    deleted_count INT := 0;
    batch_count INT;
    cutoff TIMESTAMPTZ := NOW() - (days_to_keep || ' days')::INTERVAL;
BEGIN
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
