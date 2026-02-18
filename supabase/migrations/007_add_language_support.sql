-- Migration: Add language support for multi-language RSS feeds
--
-- Adds a language column (ISO 639-1 codes like 'en', 'pt', 'es') to both
-- sources and articles tables. Articles inherit language from their source
-- at insert time, following the same pattern as category_id.
--
-- All existing sources/articles default to 'en' (English).
-- VARCHAR(5) allows future region codes like 'pt-BR' if needed.

-- =============================================================================
-- Add language column to sources
-- =============================================================================
ALTER TABLE public.sources
    ADD COLUMN language VARCHAR(5) NOT NULL DEFAULT 'en';

-- =============================================================================
-- Add language column to articles
-- =============================================================================
ALTER TABLE public.articles
    ADD COLUMN language VARCHAR(5) NOT NULL DEFAULT 'en';

-- =============================================================================
-- Add indexes for language filtering
-- =============================================================================
CREATE INDEX idx_articles_language ON public.articles (language);
CREATE INDEX idx_articles_language_published ON public.articles (language, published_at DESC);

-- =============================================================================
-- Recreate articles_with_source view to include language
-- =============================================================================
-- Must DROP and recreate because PostgreSQL doesn't allow adding columns
-- in the middle of a view with CREATE OR REPLACE
DROP VIEW IF EXISTS public.articles_with_source;

CREATE VIEW public.articles_with_source AS
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
    a.language,
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
FROM public.articles a
LEFT JOIN public.sources s ON a.source_id = s.id
LEFT JOIN public.categories c ON a.category_id = c.id;

-- Re-apply security_invoker (lost when view is recreated, per migration 005)
ALTER VIEW public.articles_with_source SET (security_invoker = on);
