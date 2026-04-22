-- Migration 019: Add fetch-state columns to sources
--
-- Three resilience improvements need new per-source state:
--
-- 1. Conditional GET — `etag` and `last_modified` capture the validators
--    returned on a successful fetch so the next run can send If-None-Match /
--    If-Modified-Since and let the server reply 304 Not Modified when nothing
--    changed. Saves bandwidth for us and the publisher.
--
-- 2. Circuit breaker — `consecutive_failures` counts repeated errors for a
--    source. Once it crosses the CIRCUIT_FAILURE_THRESHOLD, the worker sets
--    `circuit_open_until` to a cool-off timestamp; GetActiveSources() skips
--    the source until the window elapses. Prevents the 2h cron from hammering
--    feeds that are broken/gone.

ALTER TABLE sources
  ADD COLUMN IF NOT EXISTS etag TEXT,
  ADD COLUMN IF NOT EXISTS last_modified TEXT,
  ADD COLUMN IF NOT EXISTS consecutive_failures INT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS circuit_open_until TIMESTAMPTZ;

-- Partial index on open circuits — typically NULL (healthy), so this stays
-- tiny. Keeps the GetActiveSources() filter cheap.
CREATE INDEX IF NOT EXISTS idx_sources_circuit_open
  ON sources (circuit_open_until)
  WHERE circuit_open_until IS NOT NULL;
