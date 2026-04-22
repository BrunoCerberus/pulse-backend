-- Migration 020: Source health infra
--
-- Two surfaces on top of the columns added in 019:
--
-- 1. batch_update_source_fetch_state(updates JSONB) — per-source UPDATE
--    executed in a single round-trip. Lets the worker persist different
--    states across sources in one fetch cycle (fresh ETag on one source,
--    incremented failure count on another) without N PATCH requests.
--    Mirrors batch_update_article_images (014) and bump_backfill_attempts (018).
--
-- 2. source_health view — operational snapshot exposing circuit state and
--    content-freshness for the watchdog workflow and the api-source-health
--    Edge Function. security_invoker=on so RLS on sources/articles is
--    honored by callers.

CREATE OR REPLACE FUNCTION batch_update_source_fetch_state(updates JSONB)
RETURNS INTEGER AS $$
DECLARE updated_count INTEGER;
BEGIN
  WITH updated AS (
    UPDATE sources s
    SET
      etag                 = u.etag,
      last_modified        = u.last_modified,
      consecutive_failures = u.consecutive_failures,
      circuit_open_until   = u.circuit_open_until,
      last_fetched_at      = COALESCE(u.last_fetched_at, s.last_fetched_at),
      updated_at           = NOW()
    FROM jsonb_to_recordset(updates) AS u(
      id                   UUID,
      etag                 TEXT,
      last_modified        TEXT,
      consecutive_failures INT,
      circuit_open_until   TIMESTAMPTZ,
      last_fetched_at      TIMESTAMPTZ
    )
    WHERE s.id = u.id
    RETURNING 1
  )
  SELECT count(*) INTO updated_count FROM updated;
  RETURN updated_count;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = public;

-- Service role only; anon should not be able to flip circuit state.
REVOKE ALL ON FUNCTION batch_update_source_fetch_state(JSONB) FROM PUBLIC;
REVOKE ALL ON FUNCTION batch_update_source_fetch_state(JSONB) FROM anon;


-- View: aggregate fetch state with content-freshness signals so watchdog +
-- future dashboards don't each reimplement the same derivations.
CREATE OR REPLACE VIEW source_health AS
SELECT
  s.id,
  s.name,
  s.slug,
  s.is_active,
  s.consecutive_failures,
  s.circuit_open_until,
  (s.circuit_open_until IS NOT NULL AND s.circuit_open_until > NOW()) AS circuit_open,
  s.last_fetched_at,
  (SELECT MAX(a.published_at) FROM articles a WHERE a.source_id = s.id) AS most_recent_article_at,
  (SELECT COUNT(*) FROM articles a
     WHERE a.source_id = s.id AND a.published_at > NOW() - INTERVAL '24 hours') AS articles_last_24h
FROM sources s;

-- Respect caller RLS (matches articles_with_source precedent in migration 005).
ALTER VIEW source_health SET (security_invoker = on);

GRANT SELECT ON source_health TO anon, authenticated;
