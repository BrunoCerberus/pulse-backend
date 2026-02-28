-- Migration 017: Add denormalized columns, backfill, and recreate view.
-- 016 was repaired as applied but its DDL was rolled back on failure,
-- so we re-add the columns here with IF NOT EXISTS.

ALTER TABLE articles ADD COLUMN IF NOT EXISTS source_name VARCHAR(255);
ALTER TABLE articles ADD COLUMN IF NOT EXISTS source_slug VARCHAR(100);
ALTER TABLE articles ADD COLUMN IF NOT EXISTS category_name VARCHAR(100);
ALTER TABLE articles ADD COLUMN IF NOT EXISTS category_slug VARCHAR(100);

-- Backfill in batches of 2000 (each is a separate statement with its own timeout)

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

UPDATE articles a SET
  source_name = s.name, source_slug = s.slug,
  category_name = c.name, category_slug = c.slug
FROM sources s LEFT JOIN categories c ON s.category_id = c.id
WHERE a.source_id = s.id AND a.source_name IS NULL
  AND a.id IN (SELECT id FROM articles WHERE source_name IS NULL LIMIT 2000);

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
