-- Migration: Update articles_with_source view to include media fields
-- This adds podcast/video support to the API by exposing media columns
-- that were added in migration 002_add_media_support.sql
--
-- Note: Must DROP and recreate because PostgreSQL doesn't allow adding
-- columns in the middle of a view with CREATE OR REPLACE

DROP VIEW IF EXISTS articles_with_source;

CREATE VIEW articles_with_source AS
SELECT
    a.id,
    a.title,
    a.summary,
    a.content,
    a.url,
    a.image_url,
    a.thumbnail_url,
    a.author,
    a.published_at,
    a.created_at,
    -- Source info
    s.name as source_name,
    s.slug as source_slug,
    s.logo_url as source_logo_url,
    s.website_url as source_website_url,
    -- Category info
    c.name as category_name,
    c.slug as category_slug,
    -- Media fields (for podcasts/videos)
    a.media_type,
    a.media_url,
    a.media_duration,
    a.media_mime_type
FROM articles a
LEFT JOIN sources s ON a.source_id = s.id
LEFT JOIN categories c ON a.category_id = c.id;
