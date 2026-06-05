-- Migration 035: defence-in-depth write REVOKE on sources + categories
--
-- L7 from the 2026-06 security audit. Writes to `sources` and `categories` are
-- blocked TODAY purely by RLS being enabled (migration 001) with no permissive
-- write policy (migration 005 dropped the over-broad "Service role update for
-- sources" policy that had `USING (true)`). That is correct, but it is the only
-- base table anon/authenticated can reach that lacks the belt-and-suspenders
-- table-level write REVOKE the rest of the schema uses (`articles` has
-- column-level grants; `fetch_logs` is fully revoked in migration 027).
--
-- Without this REVOKE, a single regression — an RLS-disable toggle in the
-- Supabase dashboard, or a table recreate that re-enables the default PUBLIC
-- grants — would hand anon `INSERT/UPDATE/DELETE` on `sources`: flip
-- `is_active`, push `circuit_open_until` far into the future (full ingestion
-- outage), or rewrite metadata. This makes the GRANT boundary, not just RLS,
-- the thing that blocks writes — matching the defence-in-depth posture
-- migrations 027/034 established for `articles`/`sources` reads.
--
-- The worker writes `sources` only via the SECURITY DEFINER service-role RPC
-- `batch_update_source_fetch_state` (and never writes `categories`), so the
-- service_role grant is untouched and nothing legitimate breaks.
--
-- Asserted by security_invariants.sql INVARIANT 9.

-- Revoke from PUBLIC too (not just anon/authenticated) so a lingering default
-- PUBLIC grant can't leave the privilege reachable via role inheritance.
REVOKE INSERT, UPDATE, DELETE ON public.sources     FROM PUBLIC, anon, authenticated;
REVOKE INSERT, UPDATE, DELETE ON public.categories  FROM PUBLIC, anon, authenticated;

-- service_role retains full access via its default grants (it also bypasses
-- RLS). Re-assert explicitly so a future default-privilege change can't strip
-- the worker's write path.
GRANT INSERT, UPDATE, DELETE ON public.sources    TO service_role;
GRANT INSERT, UPDATE, DELETE ON public.categories TO service_role;
