-- Migration 037: defence-in-depth write REVOKE on articles (+ TRUNCATE everywhere)
--
-- Migration 035 gave `sources` and `categories` a table-level write REVOKE so
-- the GRANT boundary, not RLS alone, blocks anon/authenticated writes. Its
-- header says `articles` was already covered by its column-level grants, but
-- those (migration 027 H6) are SELECT-only: 027 revoked SELECT and re-granted a
-- column subset, and never touched Supabase's default table-level INSERT /
-- UPDATE / DELETE / TRUNCATE grants to anon and authenticated. So on
-- `articles` — the table whose URLs every iOS client dereferences — writes are
-- blocked today by RLS alone (migration 005 dropped the permissive INSERT
-- policy). One regression, such as an RLS-disable toggle in the dashboard or a
-- table recreate, would let anyone holding the public app key insert or
-- rewrite articles and push arbitrary links to every client.
--
-- TRUNCATE is revoked on all three tables because RLS never applies to it:
-- the GRANT is its only barrier. 035 left it in place on sources/categories.
-- PostgREST cannot issue TRUNCATE, so this closes no reachable path today; it
-- keeps the privilege from depending on that.
--
-- The worker writes articles only as service_role (PostgREST insert + the
-- SECURITY DEFINER RPCs), and the iOS app and Edge Functions only read, so
-- nothing legitimate breaks. The articles_with_source view is security_invoker,
-- so writes through it are checked against these base-table privileges too.
--
-- Asserted by security_invariants.sql INVARIANT 9.

-- Revoke from PUBLIC too (not just anon/authenticated) so a lingering default
-- PUBLIC grant can't leave the privilege reachable via role inheritance.
REVOKE INSERT, UPDATE, DELETE, TRUNCATE ON public.articles FROM PUBLIC, anon, authenticated;
REVOKE TRUNCATE ON public.sources    FROM PUBLIC, anon, authenticated;
REVOKE TRUNCATE ON public.categories FROM PUBLIC, anon, authenticated;

-- service_role retains full access via its default grants (it also bypasses
-- RLS). Re-assert the articles write path explicitly so a future
-- default-privilege change can't strip the worker's inserts.
GRANT INSERT, UPDATE, DELETE ON public.articles TO service_role;
