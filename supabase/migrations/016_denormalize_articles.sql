-- Migration 016: Denormalize source/category info into articles table
-- Eliminates the need for JOINs in the articles_with_source view,
-- reducing query cost on every API request.

-- Add denormalized columns
ALTER TABLE articles ADD COLUMN IF NOT EXISTS source_name VARCHAR(255);
ALTER TABLE articles ADD COLUMN IF NOT EXISTS source_slug VARCHAR(100);
ALTER TABLE articles ADD COLUMN IF NOT EXISTS category_name VARCHAR(100);
ALTER TABLE articles ADD COLUMN IF NOT EXISTS category_slug VARCHAR(100);

-- Populate from existing data in batches to avoid statement timeout
DO $$
DECLARE
  batch_size INT := 5000;
  rows_updated INT;
BEGIN
  LOOP
    UPDATE articles a SET
      source_name = s.name,
      source_slug = s.slug,
      category_name = c.name,
      category_slug = c.slug
    FROM sources s
    LEFT JOIN categories c ON s.category_id = c.id
    WHERE a.source_id = s.id
      AND a.source_name IS NULL
      AND a.id IN (
        SELECT id FROM articles WHERE source_name IS NULL LIMIT batch_size
      );
    GET DIAGNOSTICS rows_updated = ROW_COUNT;
    EXIT WHEN rows_updated = 0;
    RAISE NOTICE 'Updated % rows', rows_updated;
  END LOOP;
END $$;

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
