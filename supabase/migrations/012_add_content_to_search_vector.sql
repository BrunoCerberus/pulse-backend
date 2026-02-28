-- Migration 012: Add content to search vector
-- Includes article content (weight C) in full-text search alongside title (A) and summary (B).
-- NOTE: This briefly locks the articles table during column rebuild. Run during low traffic.

DROP INDEX IF EXISTS idx_articles_search;

ALTER TABLE public.articles DROP COLUMN search_vector;

ALTER TABLE public.articles ADD COLUMN search_vector tsvector GENERATED ALWAYS AS (
  setweight(to_tsvector('english', coalesce(title, '')), 'A') ||
  setweight(to_tsvector('english', coalesce(summary, '')), 'B') ||
  setweight(to_tsvector('english', coalesce(content, '')), 'C')
) STORED;

CREATE INDEX idx_articles_search ON public.articles USING GIN(search_vector);
