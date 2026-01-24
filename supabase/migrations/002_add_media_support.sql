-- Migration: Add podcast and video support
-- This migration adds columns to support media content (podcasts, videos)
-- and creates new categories for organizing multimedia sources.

-- Add media-related columns to articles table
ALTER TABLE articles ADD COLUMN IF NOT EXISTS media_type VARCHAR(20);
ALTER TABLE articles ADD COLUMN IF NOT EXISTS media_url TEXT;
ALTER TABLE articles ADD COLUMN IF NOT EXISTS media_duration INT;
ALTER TABLE articles ADD COLUMN IF NOT EXISTS media_mime_type VARCHAR(50);

-- Add index for filtering by media type
CREATE INDEX IF NOT EXISTS idx_articles_media_type ON articles(media_type);

-- Add new categories for podcasts and videos
INSERT INTO categories (name, slug, display_order) VALUES
    ('Podcasts', 'podcasts', 9),
    ('Videos', 'videos', 10)
ON CONFLICT (slug) DO NOTHING;

-- Add comment for documentation
COMMENT ON COLUMN articles.media_type IS 'Type of content: podcast, video, or NULL for articles';
COMMENT ON COLUMN articles.media_url IS 'Direct URL to audio/video file from RSS enclosure';
COMMENT ON COLUMN articles.media_duration IS 'Duration in seconds';
COMMENT ON COLUMN articles.media_mime_type IS 'MIME type of media file (e.g., audio/mpeg, video/mp4)';
