-- Migration 030: per-source content length cap on sources
--
-- Today every source's articles share the parser's hard-coded 200_000-rune
-- content ceiling (maxContentLen in rss-worker/internal/parser/parser.go).
-- That's the right defence-in-depth limit for a hostile feed, but it's a
-- coarse knob: a tabloid source publishing hundreds of low-substance items
-- per day pays the same per-row content cost as a long-form tech blog
-- whose article body is genuinely useful to keep around.
--
-- This migration adds an optional per-source override. The worker reads it
-- and clamps the effective content cap to MIN(source.max_content_length,
-- global maxContentLen) at both the initial-parse site and the content
-- backfill site, so a misconfigured UPDATE setting a source to 50 MB still
-- can't escape the global ceiling.
--
-- Default NULL = "use global only", which matches existing behaviour for
-- every pre-seeded source. Operators set a smaller value (e.g. 5000) via
-- a one-off UPDATE for sources where shorter content is acceptable.
--
-- No backfill of existing rows needed: this only affects future writes.
-- Reversible: ALTER TABLE public.sources DROP COLUMN max_content_length;

ALTER TABLE public.sources ADD COLUMN IF NOT EXISTS max_content_length INTEGER DEFAULT NULL;

COMMENT ON COLUMN public.sources.max_content_length IS
    'Optional per-source content length cap (runes). NULL = use global maxContentLen from the worker. Clamped to MIN(this, global) so a misconfigured large value cannot escape the global ceiling.';
