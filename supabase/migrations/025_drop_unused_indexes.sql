-- Migration 025: Drop indexes with zero usage since the stats were reset
--
-- pg_stat_user_indexes shows the indexes below have idx_scan = 0 in the
-- 112-day window covered by the current stats. They contribute write
-- amplification on every article insert/update without paying off any read.
--
-- Removed:
--
--   * idx_articles_category_published — composite (category_id, published_at)
--     added in migration 006. The /api-articles endpoint is overwhelmingly
--     filtered by source, not category-only, and idx_articles_source_published
--     (kept) handles the source path.
--   * idx_articles_media_type — added in migration 002, but no query has
--     filtered on media_type alone since the stats reset.
--   * idx_articles_image_backfill_candidates,
--     idx_articles_content_backfill_candidates — partial indexes added in
--     migration 018. The backfill candidate set is small enough that the
--     planner has not found these worth using. Recreate later if backfill
--     queue depth grows.
--   * idx_sources_circuit_open — partial index from migration 019. The
--     sources table is ~130 rows; seq scan is cheaper than the index. The
--     `GetActiveSources()` filter checks both IS NULL and < now, and a
--     partial index over IS NOT NULL only covers half of that.
--
-- Reversible: re-running the originating migrations restores the indexes.

DROP INDEX IF EXISTS idx_articles_category_published;
DROP INDEX IF EXISTS idx_articles_media_type;
DROP INDEX IF EXISTS idx_articles_image_backfill_candidates;
DROP INDEX IF EXISTS idx_articles_content_backfill_candidates;
DROP INDEX IF EXISTS idx_sources_circuit_open;
