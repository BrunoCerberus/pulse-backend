-- Migration 017: Backfill denormalized columns and recreate view.
-- Also recreates the view in case 016 was repaired before the view was created.
-- Each UPDATE targets one source_id, keeping each statement fast and
-- well within Supabase's statement timeout.
-- New articles inserted after migration 016 already have these fields set
-- by the Go worker, so this only needs to run once for historical data.

UPDATE articles a SET
  source_name = s.name, source_slug = s.slug,
  category_name = c.name, category_slug = c.slug
FROM sources s LEFT JOIN categories c ON s.category_id = c.id
WHERE a.source_id = s.id AND a.source_name IS NULL AND a.source_id = s.id
  AND a.id IN (SELECT id FROM articles WHERE source_name IS NULL LIMIT 2000);

UPDATE articles a SET
  source_name = s.name, source_slug = s.slug,
  category_name = c.name, category_slug = c.slug
FROM sources s LEFT JOIN categories c ON s.category_id = c.id
WHERE a.source_id = s.id AND a.source_name IS NULL
  AND a.id IN (SELECT id FROM articles WHERE source_name IS NULL LIMIT 2000);

UPDATE articles a SET
  source_name = s.name, source_slug = s.slug,
  category_name = c.name, category_slug = c.slug
FROM sources s LEFT JOIN categories c ON s.category_id = c.id
WHERE a.source_id = s.id AND a.source_name IS NULL
  AND a.id IN (SELECT id FROM articles WHERE source_name IS NULL LIMIT 2000);

UPDATE articles a SET
  source_name = s.name, source_slug = s.slug,
  category_name = c.name, category_slug = c.slug
FROM sources s LEFT JOIN categories c ON s.category_id = c.id
WHERE a.source_id = s.id AND a.source_name IS NULL
  AND a.id IN (SELECT id FROM articles WHERE source_name IS NULL LIMIT 2000);

UPDATE articles a SET
  source_name = s.name, source_slug = s.slug,
  category_name = c.name, category_slug = c.slug
FROM sources s LEFT JOIN categories c ON s.category_id = c.id
WHERE a.source_id = s.id AND a.source_name IS NULL
  AND a.id IN (SELECT id FROM articles WHERE source_name IS NULL LIMIT 2000);

UPDATE articles a SET
  source_name = s.name, source_slug = s.slug,
  category_name = c.name, category_slug = c.slug
FROM sources s LEFT JOIN categories c ON s.category_id = c.id
WHERE a.source_id = s.id AND a.source_name IS NULL
  AND a.id IN (SELECT id FROM articles WHERE source_name IS NULL LIMIT 2000);

UPDATE articles a SET
  source_name = s.name, source_slug = s.slug,
  category_name = c.name, category_slug = c.slug
FROM sources s LEFT JOIN categories c ON s.category_id = c.id
WHERE a.source_id = s.id AND a.source_name IS NULL
  AND a.id IN (SELECT id FROM articles WHERE source_name IS NULL LIMIT 2000);

UPDATE articles a SET
  source_name = s.name, source_slug = s.slug,
  category_name = c.name, category_slug = c.slug
FROM sources s LEFT JOIN categories c ON s.category_id = c.id
WHERE a.source_id = s.id AND a.source_name IS NULL
  AND a.id IN (SELECT id FROM articles WHERE source_name IS NULL LIMIT 2000);

UPDATE articles a SET
  source_name = s.name, source_slug = s.slug,
  category_name = c.name, category_slug = c.slug
FROM sources s LEFT JOIN categories c ON s.category_id = c.id
WHERE a.source_id = s.id AND a.source_name IS NULL
  AND a.id IN (SELECT id FROM articles WHERE source_name IS NULL LIMIT 2000);

UPDATE articles a SET
  source_name = s.name, source_slug = s.slug,
  category_name = c.name, category_slug = c.slug
FROM sources s LEFT JOIN categories c ON s.category_id = c.id
WHERE a.source_id = s.id AND a.source_name IS NULL
  AND a.id IN (SELECT id FROM articles WHERE source_name IS NULL LIMIT 2000);

-- Final sweep for any remaining rows
UPDATE articles a SET
  source_name = s.name, source_slug = s.slug,
  category_name = c.name, category_slug = c.slug
FROM sources s LEFT JOIN categories c ON s.category_id = c.id
WHERE a.source_id = s.id AND a.source_name IS NULL;

-- Recreate view without JOINs (idempotent)
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
