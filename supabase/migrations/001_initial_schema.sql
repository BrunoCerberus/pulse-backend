-- Pulse Backend Database Schema
-- Run this in Supabase SQL Editor

-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- =============================================================================
-- CATEGORIES TABLE
-- =============================================================================
CREATE TABLE categories (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(100) NOT NULL UNIQUE,
    slug VARCHAR(100) NOT NULL UNIQUE,
    display_order INT DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Insert default categories (matching Pulse app's NewsCategory)
INSERT INTO categories (name, slug, display_order) VALUES
    ('World', 'world', 1),
    ('Technology', 'technology', 2),
    ('Business', 'business', 3),
    ('Sports', 'sports', 4),
    ('Entertainment', 'entertainment', 5),
    ('Science', 'science', 6),
    ('Health', 'health', 7),
    ('Politics', 'politics', 8);

-- =============================================================================
-- SOURCES TABLE
-- =============================================================================
CREATE TABLE sources (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    slug VARCHAR(100) NOT NULL UNIQUE,
    feed_url TEXT NOT NULL,
    website_url TEXT,
    logo_url TEXT,
    category_id UUID REFERENCES categories(id),
    is_active BOOLEAN DEFAULT true,
    fetch_interval_minutes INT DEFAULT 15,
    last_fetched_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Insert default RSS sources
INSERT INTO sources (name, slug, feed_url, website_url, category_id, is_active) VALUES
    -- Guardian
    ('The Guardian - World', 'guardian-world', 'https://www.theguardian.com/world/rss', 'https://www.theguardian.com',
     (SELECT id FROM categories WHERE slug = 'world'), true),
    ('The Guardian - Technology', 'guardian-tech', 'https://www.theguardian.com/technology/rss', 'https://www.theguardian.com',
     (SELECT id FROM categories WHERE slug = 'technology'), true),
    ('The Guardian - Business', 'guardian-business', 'https://www.theguardian.com/business/rss', 'https://www.theguardian.com',
     (SELECT id FROM categories WHERE slug = 'business'), true),
    ('The Guardian - Sport', 'guardian-sport', 'https://www.theguardian.com/sport/rss', 'https://www.theguardian.com',
     (SELECT id FROM categories WHERE slug = 'sports'), true),
    ('The Guardian - Science', 'guardian-science', 'https://www.theguardian.com/science/rss', 'https://www.theguardian.com',
     (SELECT id FROM categories WHERE slug = 'science'), true),

    -- BBC
    ('BBC News - World', 'bbc-world', 'https://feeds.bbci.co.uk/news/world/rss.xml', 'https://www.bbc.com/news',
     (SELECT id FROM categories WHERE slug = 'world'), true),
    ('BBC News - Technology', 'bbc-tech', 'https://feeds.bbci.co.uk/news/technology/rss.xml', 'https://www.bbc.com/news',
     (SELECT id FROM categories WHERE slug = 'technology'), true),
    ('BBC News - Business', 'bbc-business', 'https://feeds.bbci.co.uk/news/business/rss.xml', 'https://www.bbc.com/news',
     (SELECT id FROM categories WHERE slug = 'business'), true),
    ('BBC News - Health', 'bbc-health', 'https://feeds.bbci.co.uk/news/health/rss.xml', 'https://www.bbc.com/news',
     (SELECT id FROM categories WHERE slug = 'health'), true),

    -- NPR
    ('NPR - News', 'npr-news', 'https://feeds.npr.org/1001/rss.xml', 'https://www.npr.org',
     (SELECT id FROM categories WHERE slug = 'world'), true),

    -- Tech Sources
    ('Ars Technica', 'ars-technica', 'https://feeds.arstechnica.com/arstechnica/index', 'https://arstechnica.com',
     (SELECT id FROM categories WHERE slug = 'technology'), true),
    ('TechCrunch', 'techcrunch', 'https://techcrunch.com/feed/', 'https://techcrunch.com',
     (SELECT id FROM categories WHERE slug = 'technology'), true),
    ('The Verge', 'the-verge', 'https://www.theverge.com/rss/index.xml', 'https://www.theverge.com',
     (SELECT id FROM categories WHERE slug = 'technology'), true),

    -- Science
    ('Science Daily', 'science-daily', 'https://www.sciencedaily.com/rss/all.xml', 'https://www.sciencedaily.com',
     (SELECT id FROM categories WHERE slug = 'science'), true);

-- =============================================================================
-- ARTICLES TABLE
-- =============================================================================
CREATE TABLE articles (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

    -- Content
    title TEXT NOT NULL,
    summary TEXT,
    content TEXT,
    url TEXT NOT NULL,
    url_hash VARCHAR(64) NOT NULL UNIQUE, -- SHA256 hash for deduplication

    -- Media
    image_url TEXT,
    thumbnail_url TEXT,

    -- Metadata
    author VARCHAR(255),
    source_id UUID REFERENCES sources(id) ON DELETE CASCADE,
    category_id UUID REFERENCES categories(id),

    -- Timestamps
    published_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),

    -- For full-text search
    search_vector tsvector GENERATED ALWAYS AS (
        setweight(to_tsvector('english', coalesce(title, '')), 'A') ||
        setweight(to_tsvector('english', coalesce(summary, '')), 'B')
    ) STORED
);

-- Indexes for performance
CREATE INDEX idx_articles_published_at ON articles(published_at DESC);
CREATE INDEX idx_articles_source_id ON articles(source_id);
CREATE INDEX idx_articles_category_id ON articles(category_id);
CREATE INDEX idx_articles_url_hash ON articles(url_hash);
CREATE INDEX idx_articles_created_at ON articles(created_at DESC);
CREATE INDEX idx_articles_search ON articles USING GIN(search_vector);

-- =============================================================================
-- FETCH LOGS TABLE (for monitoring)
-- =============================================================================
CREATE TABLE fetch_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    sources_processed INT DEFAULT 0,
    articles_fetched INT DEFAULT 0,
    articles_inserted INT DEFAULT 0,
    articles_skipped INT DEFAULT 0,
    errors JSONB DEFAULT '[]'::jsonb,
    status VARCHAR(20) DEFAULT 'running' -- running, completed, failed
);

CREATE INDEX idx_fetch_logs_started_at ON fetch_logs(started_at DESC);

-- =============================================================================
-- ROW LEVEL SECURITY (RLS)
-- =============================================================================

-- Enable RLS
ALTER TABLE categories ENABLE ROW LEVEL SECURITY;
ALTER TABLE sources ENABLE ROW LEVEL SECURITY;
ALTER TABLE articles ENABLE ROW LEVEL SECURITY;
ALTER TABLE fetch_logs ENABLE ROW LEVEL SECURITY;

-- Public read access (uses anon key from iOS app)
CREATE POLICY "Public read access for categories" ON categories FOR SELECT USING (true);
CREATE POLICY "Public read access for sources" ON sources FOR SELECT USING (is_active = true);
CREATE POLICY "Public read access for articles" ON articles FOR SELECT USING (true);

-- Service role for writes (used by Go worker with service_role key)
CREATE POLICY "Service role insert for articles" ON articles FOR INSERT WITH CHECK (true);
CREATE POLICY "Service role insert for fetch_logs" ON fetch_logs FOR ALL USING (true);
CREATE POLICY "Service role update for sources" ON sources FOR UPDATE USING (true);

-- =============================================================================
-- HELPER FUNCTIONS
-- =============================================================================

-- Function to clean up old articles (called by worker)
CREATE OR REPLACE FUNCTION cleanup_old_articles(days_to_keep INT DEFAULT 30)
RETURNS INT AS $$
DECLARE
    deleted_count INT;
BEGIN
    DELETE FROM articles
    WHERE created_at < NOW() - (days_to_keep || ' days')::INTERVAL;

    GET DIAGNOSTICS deleted_count = ROW_COUNT;
    RETURN deleted_count;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER;

-- Full-text search function
CREATE OR REPLACE FUNCTION search_articles(search_query TEXT, result_limit INT DEFAULT 20)
RETURNS SETOF articles AS $$
BEGIN
    RETURN QUERY
    SELECT *
    FROM articles
    WHERE search_vector @@ plainto_tsquery('english', search_query)
    ORDER BY ts_rank(search_vector, plainto_tsquery('english', search_query)) DESC,
             published_at DESC
    LIMIT result_limit;
END;
$$ LANGUAGE plpgsql;

-- =============================================================================
-- VIEWS
-- =============================================================================

-- View for articles with source info (convenient for API)
CREATE OR REPLACE VIEW articles_with_source AS
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
    s.name as source_name,
    s.slug as source_slug,
    s.logo_url as source_logo_url,
    s.website_url as source_website_url,
    c.name as category_name,
    c.slug as category_slug
FROM articles a
LEFT JOIN sources s ON a.source_id = s.id
LEFT JOIN categories c ON a.category_id = c.id;
