-- Migration 023: Data cleanup — inactivate sources that are clearly dead
--
-- After PR #34 (widened staleness window to 7d) and PR #35 (raised
-- MAX_STALE to 30), the watchdog stopped false-paging but the underlying
-- problem remained: the fleet contained ~20 sources that were genuinely
-- broken or abandoned. They polluted every health snapshot and required
-- the threshold to be loosened well past where it should sit for a
-- healthy fleet.
--
-- This migration flips `is_active = false` on two well-defined cohorts.
-- Both criteria are conservative — even quarterly podcasts and recently-
-- added sources are spared. Sources marked inactive here can be revived
-- manually if the upstream feed comes back; the data isn't deleted.
--
-- Cohort 1: most recent article is older than 180 days.
--   At 6 months without a single article, the source is either dead
--   (e.g. Cafe da Manha's last article in 2020-05) or has changed feed
--   URL without us noticing. Either way, not useful in the active fleet.
--
-- Cohort 2: never produced an article AND has tripped the circuit
-- breaker at least once (consecutive_failures >= 5).
--   A source that has failed 5+ consecutive fetches without ever
--   surfacing content has a broken endpoint — wrong URL, dead host,
--   geo-blocked, or auth-walled. The threshold matches
--   CIRCUIT_FAILURE_THRESHOLD so we only inactivate sources that have
--   already been quarantined by the breaker.
--
-- Both cohorts AND in is_active = true so re-running this migration on a
-- restored backup is naturally a no-op for already-inactivated rows. The
-- final RAISE NOTICE prints the inactivated names so the audit trail
-- ends up in the supabase db push log.

DO $$
DECLARE
    inactivated_names TEXT[];
    inactivated_count INT;
BEGIN
    WITH dead AS (
        UPDATE public.sources s
        SET
            is_active  = false,
            updated_at = NOW()
        WHERE s.is_active = true
          AND (
              -- Cohort 1: stale beyond any reasonable cadence.
              (
                  EXISTS (SELECT 1 FROM public.articles a WHERE a.source_id = s.id)
                  AND (
                      SELECT MAX(a.published_at)
                      FROM public.articles a
                      WHERE a.source_id = s.id
                  ) < NOW() - INTERVAL '180 days'
              )
              OR
              -- Cohort 2: never produced + circuit-trip-eligible failures.
              (
                  NOT EXISTS (SELECT 1 FROM public.articles a WHERE a.source_id = s.id)
                  AND s.consecutive_failures >= 5
              )
          )
        RETURNING s.name
    )
    SELECT array_agg(name ORDER BY name), count(*)
    INTO inactivated_names, inactivated_count
    FROM dead;

    RAISE NOTICE 'Migration 023: inactivated % source(s): %',
        COALESCE(inactivated_count, 0),
        COALESCE(array_to_string(inactivated_names, ', '), '(none)');
END $$;
