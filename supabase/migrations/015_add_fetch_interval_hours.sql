-- Migration 015: Add fetch_interval_hours column to sources
-- Allows different fetch frequencies per source.
-- Default is 2 hours (matching the current cron schedule).
-- Podcasts and videos are set to 6 hours since they publish less frequently.

ALTER TABLE sources ADD COLUMN IF NOT EXISTS fetch_interval_hours INTEGER NOT NULL DEFAULT 2;

-- Set low-volume sources (podcasts, videos) to fetch less often
UPDATE sources SET fetch_interval_hours = 6 WHERE category_id IN (
  SELECT id FROM categories WHERE slug IN ('podcasts', 'videos')
);
