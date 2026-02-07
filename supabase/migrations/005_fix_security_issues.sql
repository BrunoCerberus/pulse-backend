-- Migration: Fix 6 security issues flagged by Supabase Security Advisor
--
-- 1. View `articles_with_source` bypasses RLS (default PostgreSQL behavior)
--    Fix: Enable security_invoker so the view respects caller's RLS policies
--
-- 2-3. Functions have mutable search_path (search_path injection risk)
--    Fix: Pin search_path on both functions
--
-- 4-6. RLS policies named "Service role..." but apply to ALL roles including anon
--    The service_role key already bypasses RLS, so these policies are unnecessary
--    and accidentally grant anon users write access.
--    Fix: Drop the overly permissive policies

-- =============================================================================
-- FIX 1: View security_invoker (PostgreSQL 15+)
-- =============================================================================
-- By default, views execute as the view owner, bypassing RLS on underlying tables.
-- Setting security_invoker = on makes the view respect the calling user's RLS policies.
ALTER VIEW public.articles_with_source SET (security_invoker = on);

-- =============================================================================
-- FIX 2-3: Pin search_path on functions
-- =============================================================================
-- A mutable search_path allows callers to manipulate which schema objects the
-- function resolves, potentially redirecting queries to attacker-controlled tables.
-- This is especially critical for cleanup_old_articles which is SECURITY DEFINER.

CREATE OR REPLACE FUNCTION public.cleanup_old_articles(days_to_keep INT DEFAULT 30)
RETURNS INT AS $$
DECLARE
    deleted_count INT;
BEGIN
    DELETE FROM public.articles
    WHERE created_at < NOW() - (days_to_keep || ' days')::INTERVAL;

    GET DIAGNOSTICS deleted_count = ROW_COUNT;
    RETURN deleted_count;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = '';

CREATE OR REPLACE FUNCTION public.search_articles(search_query TEXT, result_limit INT DEFAULT 20)
RETURNS SETOF public.articles AS $$
BEGIN
    RETURN QUERY
    SELECT *
    FROM public.articles
    WHERE search_vector @@ plainto_tsquery('english', search_query)
    ORDER BY ts_rank(search_vector, plainto_tsquery('english', search_query)) DESC,
             published_at DESC
    LIMIT result_limit;
END;
$$ LANGUAGE plpgsql SET search_path = '';

-- =============================================================================
-- FIX 4-6: Remove overly permissive RLS policies
-- =============================================================================
-- These policies are named "Service role..." but actually apply to ALL roles
-- including `anon`. The service_role key already bypasses RLS entirely, so these
-- policies serve no purpose and accidentally grant anon users write access.

DROP POLICY IF EXISTS "Service role insert for articles" ON public.articles;
DROP POLICY IF EXISTS "Service role insert for fetch_logs" ON public.fetch_logs;
DROP POLICY IF EXISTS "Service role update for sources" ON public.sources;
