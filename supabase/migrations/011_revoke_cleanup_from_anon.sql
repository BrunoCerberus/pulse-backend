-- Migration 011: Restrict cleanup_old_articles access
--
-- The cleanup_old_articles function runs as SECURITY DEFINER and can delete
-- all articles. By default, the anon and authenticated roles can execute any
-- public function via PostgREST. Revoking execute prevents unauthenticated
-- callers from triggering article deletion. Only the service_role (which
-- bypasses permission checks) will be able to call it.

REVOKE EXECUTE ON FUNCTION public.cleanup_old_articles(INT) FROM anon;
REVOKE EXECUTE ON FUNCTION public.cleanup_old_articles(INT) FROM authenticated;
