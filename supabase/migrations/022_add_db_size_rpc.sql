-- Migration 022: Expose pg_database_size as an RPC for the watchdog
--
-- The April 2026 incident (project hit free-tier 500 MB quota → service
-- restricted with HTTP 402) had no early warning. Adding this RPC lets
-- api-source-health surface the current DB size so watchdog.yml can fail
-- the job (→ GitHub email) days before the platform restriction kicks in.
--
-- pg_database_size() is a built-in safe to expose to anon: it returns a
-- single integer (total bytes), not user data. Granted to anon and
-- authenticated so the existing supabase-proxy auth flow (anon key from
-- Edge Function) works without a service-role escalation.

CREATE OR REPLACE FUNCTION public.get_db_size_bytes()
RETURNS BIGINT
LANGUAGE sql
STABLE
SET search_path = ''
AS $$
  SELECT pg_catalog.pg_database_size(pg_catalog.current_database());
$$;

REVOKE ALL ON FUNCTION public.get_db_size_bytes() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.get_db_size_bytes() TO anon, authenticated, service_role;
