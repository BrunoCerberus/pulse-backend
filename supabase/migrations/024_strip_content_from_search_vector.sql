-- Migration 024: Strip `content` from the article search vector
--
-- Reverts the content-inclusion side of migration 012. The full-text search
-- index (`idx_articles_search`) had grown to ~32 MB on a 200 MB DB while
-- serving only a handful of scans per quarter — `/api-search` is rarely hit
-- and what hits it lands in the 1-minute private cache.
--
-- Content stays in the column, but is no longer tokenized into search_vector,
-- which:
--   * shrinks the stored tsvector column (largest contributor was the C-weight
--     content tokens — articles average ~3.2 KB of content each), and
--   * shrinks the GIN index accordingly.
--
-- Search still matches title (weight A) and summary (weight B), which is what
-- the iOS app surfaces anyway.
--
-- NOTE: Briefly locks the articles table to rebuild the generated column.
-- Migration 012 took ~1m 45s on this dataset — expect similar.

DROP INDEX IF EXISTS idx_articles_search;

ALTER TABLE public.articles DROP COLUMN search_vector;

ALTER TABLE public.articles ADD COLUMN search_vector tsvector GENERATED ALWAYS AS (
  setweight(to_tsvector('english', coalesce(title, '')), 'A') ||
  setweight(to_tsvector('english', coalesce(summary, '')), 'B')
) STORED;

CREATE INDEX idx_articles_search ON public.articles USING GIN(search_vector);
