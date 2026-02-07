-- Migration: Add composite indexes for common query patterns
--
-- The articles_with_source view is queried with filters on category/source
-- and ordered by published_at DESC. Composite indexes let PostgreSQL
-- satisfy both the filter and sort in a single index scan.

-- Covers: WHERE category_id = ? ORDER BY published_at DESC
CREATE INDEX idx_articles_category_published
    ON articles (category_id, published_at DESC);

-- Covers: WHERE source_id = ? ORDER BY published_at DESC
CREATE INDEX idx_articles_source_published
    ON articles (source_id, published_at DESC);
