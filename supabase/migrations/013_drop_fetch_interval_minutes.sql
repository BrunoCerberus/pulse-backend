-- Migration 013: Drop unused fetch_interval_minutes column
-- This column is not read by any Go code or Edge Function.
-- Fetch frequency is controlled entirely by the GitHub Actions cron schedule.

ALTER TABLE public.sources DROP COLUMN IF EXISTS fetch_interval_minutes;
