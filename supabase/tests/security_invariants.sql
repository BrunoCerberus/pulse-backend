-- =============================================================================
-- security_invariants.sql
-- =============================================================================
-- Post-apply security assertions for the Pulse Backend Supabase schema.
--
-- HOW IT RUNS (see .github/workflows/migrations-ci.yml):
--   psql "<local-stack-conn>" -v ON_ERROR_STOP=1 -f supabase/tests/security_invariants.sql
--
-- Run AFTER `supabase db reset --no-seed` has applied migrations 001..035 from
-- scratch against the Supabase local stack. The local stack provisions the
-- Supabase-managed roles (anon, authenticated, service_role, authenticator) and
-- the `request.jwt.claims` request GUC that these migrations reference; a bare
-- Postgres cannot apply them, which is why this lives behind the CLI's stack.
--
-- The CI connection is `postgres` (a superuser). Every assertion below is a
-- `DO $$ ... IF NOT (<cond>) THEN RAISE EXCEPTION '<msg>'; END IF; END $$;`
-- block. With `-v ON_ERROR_STOP=1`, any RAISE aborts psql with a non-zero exit
-- and fails the CI step. Each block cites the migration(s) it guards.
--
-- Resilience note: where a migration's runtime behavior differs from a naive
-- expectation (e.g. the search_articles guard RETURNs empty rather than
-- RAISEing, and the SECURITY DEFINER caller gate keys on SESSION_USER /
-- request.jwt.claims rather than CURRENT_USER), the assertion is written to
-- match the migration AS WRITTEN, with a comment explaining the nuance. A
-- weaker-but-correct assertion is always preferred over a wrong one.
-- =============================================================================

\set ON_ERROR_STOP on


-- -----------------------------------------------------------------------------
-- INVARIANT 1: RLS is enabled on every anon-reachable base table —
--              public.articles, public.fetch_logs, public.sources,
--              public.categories.
-- -----------------------------------------------------------------------------
-- Guards migration 001 (ALTER TABLE ... ENABLE ROW LEVEL SECURITY) and the
-- defence-in-depth posture reinforced by 005 (drops the over-broad write
-- policies), 027 (column-level grants + fetch_logs REVOKE), and 035 (the
-- sources/categories write REVOKE — whose backstop only matters while RLS is
-- also on). We check the catalog flag pg_class.relrowsecurity directly so the
-- assertion is independent of which policies exist. sources/categories are
-- included because they are anon-readable and an RLS-disable regression there
-- (dashboard toggle / table recreate) would re-expose writes that 035 only
-- defends at the GRANT layer.
DO $$
DECLARE
    tbl TEXT;
BEGIN
    FOREACH tbl IN ARRAY ARRAY['articles', 'fetch_logs', 'sources', 'categories'] LOOP
        IF NOT EXISTS (
            SELECT 1
            FROM pg_catalog.pg_class c
            JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
            WHERE n.nspname = 'public'
              AND c.relname = tbl
              AND c.relrowsecurity = true
        ) THEN
            RAISE EXCEPTION 'INVARIANT 1 FAILED: RLS is not enabled on public.% (migration 001/005/027/035)', tbl;
        END IF;
    END LOOP;
END $$;


-- -----------------------------------------------------------------------------
-- INVARIANT 2: Every SECURITY DEFINER function in schema public pins an EMPTY
--              search_path (SET search_path = '').
-- -----------------------------------------------------------------------------
-- Guards migration 027's C5 fix (rebuilds 014/018/020/026's `search_path =
-- public` SECURITY DEFINER functions to `search_path = ''`, since pg_temp is
-- implicitly prepended and an attacker who can create objects there could
-- shadow built-ins) and migrations 028/031/032/033 which keep that posture.
--
-- pg_proc.proconfig is a text[] of "name=value" GUC overrides. `SET search_path
-- = ''` serializes to the array element `search_path=` (empty value); some PG
-- builds render an empty string as `search_path=""`. We accept BOTH forms and
-- FAIL any SECURITY DEFINER function that either lacks a search_path override
-- entirely or pins it to a NON-empty value (e.g. a regression back to
-- `search_path=public`).
--
-- NOTE: get_db_size_bytes (migration 022) is LANGUAGE sql and SECURITY INVOKER
-- (no SECURITY DEFINER keyword), so prosecdef = false and it is correctly NOT
-- in scope here — even though it also pins search_path = ''.
DO $$
DECLARE
    bad_fn TEXT;
BEGIN
    SELECT p.proname
    INTO bad_fn
    FROM pg_catalog.pg_proc p
    JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
    WHERE n.nspname = 'public'
      AND p.prosecdef = true
      AND NOT EXISTS (
          SELECT 1
          FROM unnest(COALESCE(p.proconfig, ARRAY[]::text[])) AS cfg
          WHERE cfg = 'search_path=' OR cfg = 'search_path=""'
      )
    LIMIT 1;

    IF bad_fn IS NOT NULL THEN
        RAISE EXCEPTION 'INVARIANT 2 FAILED: SECURITY DEFINER function public.% does not pin SET search_path = '''' (migration 027 C5)', bad_fn;
    END IF;
END $$;


-- -----------------------------------------------------------------------------
-- INVARIANT 3: The service-role write functions exist and are SECURITY
--              DEFINER.
-- -----------------------------------------------------------------------------
-- Guards migration 027 (which introduced/rebuilt the original five with an
-- in-function caller check), migration 033 (which fixed that check), and
-- migrations 031/032 (the prune_old_* pair, SECURITY DEFINER from birth with
-- the working JWT-claim gate). We match by function name in schema public and
-- require prosecdef = true. We do not over-constrain on argument types — name +
-- SECURITY DEFINER is the invariant the security model depends on.
DO $$
DECLARE
    fn TEXT;
    expected TEXT[] := ARRAY[
        'cleanup_old_articles',
        'batch_update_article_images',
        'batch_update_article_content',
        'bump_backfill_attempts',
        'batch_update_source_fetch_state',
        'prune_old_image_urls',
        'prune_old_content'
    ];
BEGIN
    FOREACH fn IN ARRAY expected LOOP
        IF NOT EXISTS (
            SELECT 1
            FROM pg_catalog.pg_proc p
            JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
            WHERE n.nspname = 'public'
              AND p.proname = fn
              AND p.prosecdef = true
        ) THEN
            RAISE EXCEPTION 'INVARIANT 3 FAILED: write function public.% is missing or not SECURITY DEFINER (migration 027/033)', fn;
        END IF;
    END LOOP;
END $$;


-- -----------------------------------------------------------------------------
-- INVARIANT 4: Every write function carries migration 033's caller gate, and
--              the GRANT boundary holds (anon/authenticated cannot EXECUTE;
--              service_role can).
-- -----------------------------------------------------------------------------
-- Guards migration 033. The gate in each migration-027/033 write function is:
--     jwt_role := NULLIF(current_setting('request.jwt.claims', true), '');
--     IF NOT ((jwt_role::jsonb ->> 'role') = 'service_role'
--             OR SESSION_USER = 'postgres') THEN RAISE EXCEPTION ...; END IF;
--
-- WHY THIS IS A CATALOG CHECK, NOT A BEHAVIORAL ONE:
--   To trip the gate you need SESSION_USER != 'postgres'. The only way to do
--   that in a single psql session is `SET SESSION AUTHORIZATION <role>`, which
--   requires the *initial* session user to be a superuser. The Supabase local
--   stack's `postgres` role is NOT a superuser, so that statement is denied
--   ("permission denied to set session authorization"). `SET ROLE` doesn't help
--   either — it changes CURRENT_USER, not SESSION_USER. So a behavioral
--   rejection test isn't possible from CI; we assert the two things that matter
--   via the catalog instead, both privilege-free:
--     (a) every write function's body contains the request.jwt.claims gate —
--         i.e. migration 033's fix, NOT the dead CURRENT_USER check from 027;
--     (b) the GRANT boundary, which migration 033 itself calls the actual
--         load-bearing security: anon + authenticated have no EXECUTE,
--         service_role does. has_function_privilege() reads the catalog.
DO $$
DECLARE
    fn       TEXT;
    fn_oid   OID;
    expected TEXT[] := ARRAY[
        'cleanup_old_articles',
        'batch_update_article_images',
        'batch_update_article_content',
        'bump_backfill_attempts',
        'batch_update_source_fetch_state',
        'prune_old_image_urls',
        'prune_old_content'
    ];
BEGIN
    FOREACH fn IN ARRAY expected LOOP
        SELECT p.oid INTO fn_oid
        FROM pg_catalog.pg_proc p
        JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
        WHERE n.nspname = 'public' AND p.proname = fn
        ORDER BY p.oid
        LIMIT 1;

        IF fn_oid IS NULL THEN
            RAISE EXCEPTION 'INVARIANT 4 FAILED: write function public.% not found (migration 033)', fn;
        END IF;

        -- (a) migration 033's JWT-claim gate is present (not the dead
        -- CURRENT_USER check it replaced, which lacked request.jwt.claims).
        IF pg_catalog.pg_get_functiondef(fn_oid) NOT LIKE '%request.jwt.claims%' THEN
            RAISE EXCEPTION 'INVARIANT 4 FAILED: public.% lacks the request.jwt.claims caller gate (regressed to the dead CURRENT_USER check?) (migration 033)', fn;
        END IF;

        -- (b) GRANT boundary — the actual security per migration 033.
        IF has_function_privilege('anon', fn_oid, 'EXECUTE') THEN
            RAISE EXCEPTION 'INVARIANT 4 FAILED: anon can EXECUTE public.% (REVOKE regressed) (migration 027/033)', fn;
        END IF;
        IF has_function_privilege('authenticated', fn_oid, 'EXECUTE') THEN
            RAISE EXCEPTION 'INVARIANT 4 FAILED: authenticated can EXECUTE public.% (REVOKE regressed) (migration 027/033)', fn;
        END IF;
        IF NOT has_function_privilege('service_role', fn_oid, 'EXECUTE') THEN
            RAISE EXCEPTION 'INVARIANT 4 FAILED: service_role cannot EXECUTE public.% (GRANT missing) (migration 027/033)', fn;
        END IF;
    END LOOP;
END $$;


-- -----------------------------------------------------------------------------
-- INVARIANT 5: search_articles input validation returns ZERO rows for empty and
--              over-length (>200 char) queries.
-- -----------------------------------------------------------------------------
-- Guards migrations 027 (introduced the length cap) and 028 (current form). The
-- function signature is public.search_articles(search_query TEXT, result_limit
-- INT DEFAULT 20).
--
-- IMPORTANT — this is a ROW-COUNT check, not an exception check. The guard in
-- the function body is:
--     IF search_query IS NULL OR length(search_query) = 0
--        OR length(search_query) > 200 THEN
--         RETURN;            -- returns an EMPTY result set; does NOT RAISE
--     END IF;
-- So an empty or 201-char query yields zero rows (bounding the tsquery-build
-- DoS surface) rather than throwing. Asserting an exception here would
-- false-fail; asserting an empty result set matches the migration as written
-- and still proves the cap is enforced.
DO $$
DECLARE
    empty_rows  BIGINT;
    long_rows   BIGINT;
    long_query  TEXT := repeat('a', 201);  -- 201 chars: one past the 200 cap
BEGIN
    SELECT count(*) INTO empty_rows FROM public.search_articles('', 20);
    IF empty_rows <> 0 THEN
        RAISE EXCEPTION 'INVARIANT 5 FAILED: search_articles('''') returned % rows; expected 0 (migration 027/028 length guard)', empty_rows;
    END IF;

    SELECT count(*) INTO long_rows FROM public.search_articles(long_query, 20);
    IF long_rows <> 0 THEN
        RAISE EXCEPTION 'INVARIANT 5 FAILED: search_articles(<201 chars>) returned % rows; expected 0 (migration 027/028 length guard)', long_rows;
    END IF;
END $$;


-- -----------------------------------------------------------------------------
-- INVARIANT 6: The articles_with_source view exists and does NOT expose
--              url_hash.
-- -----------------------------------------------------------------------------
-- Guards migration 027's H6 fix: the view was recreated to project only the
-- public column set, dropping url_hash (and the backfill-state columns) that an
-- earlier `SELECT *`-shaped view (migrations 004/016) would have leaked once
-- the column existed. We assert the view is present AND that url_hash is absent
-- from its columns.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.views
        WHERE table_schema = 'public'
          AND table_name = 'articles_with_source'
    ) THEN
        RAISE EXCEPTION 'INVARIANT 6 FAILED: view public.articles_with_source does not exist (migration 027)';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'articles_with_source'
          AND column_name = 'url_hash'
    ) THEN
        RAISE EXCEPTION 'INVARIANT 6 FAILED: view public.articles_with_source still exposes url_hash (migration 027 H6 dropped it)';
    END IF;
END $$;


-- -----------------------------------------------------------------------------
-- INVARIANT 7: anon/authenticated cannot read operational columns of
--              public.sources, but retain the public column set.
-- -----------------------------------------------------------------------------
-- Guards migration 034 (C1 from the 2026-06 security discovery sweep). Migration
-- 027 hardened articles but left `sources` with a default full-column anon
-- grant, exposing operational/reconnaissance columns (feed_url, fetch state,
-- circuit-breaker state) to the public anon key. 034 REVOKEs the table grant and
-- re-GRANTs only the api-sources column set. has_column_privilege() reads the
-- catalog, so this assertion needs no role switching (which the local stack's
-- non-superuser postgres forbids — see INVARIANT 4's note).
DO $$
DECLARE
    r           TEXT;
    col         TEXT;
    operational TEXT[] := ARRAY[
        'feed_url', 'last_fetched_at', 'fetch_interval_hours',
        'etag', 'last_modified', 'consecutive_failures',
        'circuit_open_until', 'max_content_length'
    ];
    public_cols TEXT[] := ARRAY[
        'id', 'name', 'slug', 'website_url', 'logo_url',
        'category_id', 'language', 'is_active'
    ];
BEGIN
    FOREACH r IN ARRAY ARRAY['anon', 'authenticated'] LOOP
        FOREACH col IN ARRAY operational LOOP
            IF has_column_privilege(r, 'public.sources', col, 'SELECT') THEN
                RAISE EXCEPTION 'INVARIANT 7 FAILED: % can SELECT operational column public.sources.% (migration 034 column-grant regressed)', r, col;
            END IF;
        END LOOP;
        FOREACH col IN ARRAY public_cols LOOP
            IF NOT has_column_privilege(r, 'public.sources', col, 'SELECT') THEN
                RAISE EXCEPTION 'INVARIANT 7 FAILED: % lost SELECT on public column public.sources.% (migration 034 over-revoked)', r, col;
            END IF;
        END LOOP;
    END LOOP;
END $$;


-- -----------------------------------------------------------------------------
-- INVARIANT 8: anon/authenticated cannot read url_hash or backfill-state
--              columns of the BASE table public.articles.
-- -----------------------------------------------------------------------------
-- Guards migration 027's H6 column-level grant (REVOKE SELECT ON articles +
-- GRANT SELECT (safe cols)). INVARIANT 6 only checks the VIEW's column list,
-- but anon reaches the base table directly via PostgREST
-- (GET /rest/v1/articles?select=url_hash), so the load-bearing control is the
-- base-table column grant. A stray `GRANT SELECT ON public.articles TO anon`
-- (Supabase re-grants on table recreate; 024 does DROP/ADD COLUMN on articles)
-- would re-expose these while INVARIANT 6 stayed green. has_column_privilege
-- reads the catalog, so no role switching is needed (see INVARIANT 4's note).
DO $$
DECLARE
    r   TEXT;
    col TEXT;
    forbidden TEXT[] := ARRAY[
        'url_hash',
        'image_backfill_attempts', 'image_backfill_last_attempt_at',
        'content_backfill_attempts', 'content_backfill_last_attempt_at'
    ];
    public_sample TEXT[] := ARRAY['id', 'title', 'url'];
BEGIN
    FOREACH r IN ARRAY ARRAY['anon', 'authenticated'] LOOP
        FOREACH col IN ARRAY forbidden LOOP
            IF has_column_privilege(r, 'public.articles', col, 'SELECT') THEN
                RAISE EXCEPTION 'INVARIANT 8 FAILED: % can SELECT public.articles.% (migration 027 H6 column-grant regressed)', r, col;
            END IF;
        END LOOP;
        FOREACH col IN ARRAY public_sample LOOP
            IF NOT has_column_privilege(r, 'public.articles', col, 'SELECT') THEN
                RAISE EXCEPTION 'INVARIANT 8 FAILED: % lost SELECT on public column public.articles.% (migration 027 over-revoked)', r, col;
            END IF;
        END LOOP;
    END LOOP;
END $$;


-- -----------------------------------------------------------------------------
-- INVARIANT 9: anon/authenticated have NO INSERT/UPDATE/DELETE on
--              public.sources or public.categories.
-- -----------------------------------------------------------------------------
-- Guards migration 035 (the defence-in-depth write REVOKE). Writes are also
-- blocked by RLS (INVARIANT 1), but 035 makes the GRANT boundary block them
-- too, so an RLS-disable regression can't silently re-open writes. The worker
-- writes sources only via the service_role RPC, so this REVOKE breaks nothing.
DO $$
DECLARE
    r    TEXT;
    tbl  TEXT;
    priv TEXT;
BEGIN
    FOREACH r IN ARRAY ARRAY['anon', 'authenticated'] LOOP
        FOREACH tbl IN ARRAY ARRAY['public.sources', 'public.categories'] LOOP
            FOREACH priv IN ARRAY ARRAY['INSERT', 'UPDATE', 'DELETE'] LOOP
                IF has_table_privilege(r, tbl, priv) THEN
                    RAISE EXCEPTION 'INVARIANT 9 FAILED: % has % on % (migration 035 write-REVOKE regressed)', r, priv, tbl;
                END IF;
            END LOOP;
        END LOOP;
    END LOOP;
END $$;


-- -----------------------------------------------------------------------------
-- INVARIANT 10: search_articles is SECURITY DEFINER and keeps its bounded,
--               explicit column projection (no SETOF articles).
-- -----------------------------------------------------------------------------
-- Guards migrations 027/028. search_articles is SECURITY DEFINER and GRANTed to
-- anon, so it bypasses the column-level grants on articles. The original C1 bug
-- was `RETURNS SETOF public.articles`, which leaked url_hash / search_vector /
-- backfill state to anon. The current form is an explicit `RETURNS TABLE(...)`
-- with only the safe columns. A regression back to SETOF (or one that adds a
-- sensitive column to the TABLE list) would re-open the leak while every other
-- invariant stayed green. pg_get_function_result renders the RETURNS clause:
-- 'TABLE(...)' for the explicit projection, 'SETOF articles' for the leak.
--
-- Also asserts statement_timeout = '3s' is pinned via proconfig (F1 from audit).
DO $$
DECLARE
    fn_oid  OID;
    result  TEXT;
    has_timeout BOOLEAN;
BEGIN
    SELECT p.oid INTO fn_oid
    FROM pg_catalog.pg_proc p
    JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
    WHERE n.nspname = 'public' AND p.proname = 'search_articles'
    ORDER BY p.oid
    LIMIT 1;

    IF fn_oid IS NULL THEN
        RAISE EXCEPTION 'INVARIANT 10 FAILED: public.search_articles not found (migration 027/028)';
    END IF;

    IF NOT (SELECT prosecdef FROM pg_catalog.pg_proc WHERE oid = fn_oid) THEN
        RAISE EXCEPTION 'INVARIANT 10 FAILED: public.search_articles is not SECURITY DEFINER (migration 027)';
    END IF;

    result := pg_catalog.pg_get_function_result(fn_oid);
    IF result NOT LIKE 'TABLE(%' THEN
        RAISE EXCEPTION 'INVARIANT 10 FAILED: search_articles no longer returns an explicit TABLE projection (regressed to %?) — possible SETOF leak (migration 027/028 C1)', result;
    END IF;
    IF result LIKE '%url_hash%'
       OR result LIKE '%search_vector%'
       OR result LIKE '%backfill%' THEN
        RAISE EXCEPTION 'INVARIANT 10 FAILED: search_articles projection leaks a sensitive column: %', result;
    END IF;

    -- F1: Verify statement_timeout is still pinned (3s cap on search DoS surface).
    SELECT EXISTS(
        SELECT 1 FROM unnest(COALESCE(p.proconfig, ARRAY[]::text[])) AS cfg
        WHERE cfg LIKE 'statement_timeout=3s'
    ) INTO has_timeout FROM pg_catalog.pg_proc p WHERE p.oid = fn_oid;

    IF NOT has_timeout THEN
        RAISE EXCEPTION 'INVARIANT 10 FAILED: search_articles statement_timeout is not set to ''3s'' (audit F1) — tsquery build could exhaust CPU without timeout';
    END IF;
END $$;


-- -----------------------------------------------------------------------------
-- INVARIANT 11: the security_invoker views run with security_invoker = on.
-- -----------------------------------------------------------------------------
-- Guards migrations 005/027 (articles_with_source) and 020/027 (source_health).
-- security_invoker = on makes a view honor the CALLER's RLS + column grants
-- rather than the view owner's. The flag has historically been lost on a view
-- recreate (e.g. 004 created articles_with_source without it; 005 re-set it).
-- If it were dropped, the view would run as owner and bypass anon's column
-- grants — re-leaking whatever the view projects. reloptions stores boolean
-- options as 'name=on' or 'name=true' depending on how they were written; we
-- accept either.
DO $$
DECLARE
    v TEXT;
    opts TEXT[];
BEGIN
    FOREACH v IN ARRAY ARRAY['articles_with_source', 'source_health'] LOOP
        SELECT c.reloptions INTO opts
        FROM pg_catalog.pg_class c
        JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'public' AND c.relname = v;

        IF opts IS NULL
           OR NOT (
               opts @> ARRAY['security_invoker=on']
               OR opts @> ARRAY['security_invoker=true']
           ) THEN
            RAISE EXCEPTION 'INVARIANT 11 FAILED: view public.% is not security_invoker=on (reloptions=%) (migration 005/020/027)', v, opts;
        END IF;
    END LOOP;
END $$;


\echo 'security_invariants: all assertions passed'
