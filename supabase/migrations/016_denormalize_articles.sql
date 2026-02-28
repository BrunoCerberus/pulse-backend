-- Migration 016: Denormalize source/category info into articles table
-- Eliminates the need for JOINs in the articles_with_source view,
-- reducing query cost on every API request.

-- Add denormalized columns
ALTER TABLE articles ADD COLUMN IF NOT EXISTS source_name VARCHAR(255);
ALTER TABLE articles ADD COLUMN IF NOT EXISTS source_slug VARCHAR(100);
ALTER TABLE articles ADD COLUMN IF NOT EXISTS category_name VARCHAR(100);
ALTER TABLE articles ADD COLUMN IF NOT EXISTS category_slug VARCHAR(100);

-- Backfill is done via 017_backfill_denormalized_articles.sql
-- (separate migration to keep each statement within timeout)

-- Recreate view without JOINs
DROP VIEW IF EXISTS articles_with_source;
CREATE VIEW articles_with_source AS
SELECT id, title, summary, content, url, image_url, thumbnail_url, author,
  published_at, created_at, language,
  source_id, category_id,
  source_name, source_slug,
  category_name, category_slug,
  media_type, media_url, media_duration, media_mime_type
FROM articles;
ALTER VIEW articles_with_source SET (security_invoker = on);
