-- Migration 018: Track backfill attempts per article
--
-- Problem: backfills for og:image and content re-fetch the same failing
-- articles on every run because the query only filters on image_url/content
-- being null. Articles that cannot be enriched (404, paywall, JS-rendered,
-- etc.) waste requests forever.
--
-- Fix: add per-article attempt counters + last-attempt timestamps so
-- backfill queries can exclude exhausted articles and respect a cooldown
-- between retries. Successful backfills naturally exit the candidate set
-- by setting image_url/content to a non-null value.

ALTER TABLE articles
  ADD COLUMN IF NOT EXISTS image_backfill_attempts INT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS image_backfill_last_attempt_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS content_backfill_attempts INT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS content_backfill_last_attempt_at TIMESTAMPTZ;

-- Partial indexes speed up candidate selection among the (small) subset of
-- articles still missing the target field.
CREATE INDEX IF NOT EXISTS idx_articles_image_backfill_candidates
  ON articles (image_backfill_last_attempt_at NULLS FIRST, image_backfill_attempts)
  WHERE image_url IS NULL;

CREATE INDEX IF NOT EXISTS idx_articles_content_backfill_candidates
  ON articles (content_backfill_last_attempt_at NULLS FIRST, content_backfill_attempts)
  WHERE content IS NULL OR content = '';

-- Batch RPC: increment the attempt counter and stamp the timestamp for a
-- list of url_hashes. `kind` picks which pair of columns to update.
-- Mirrors the shape of batch_update_article_images (migration 014).
CREATE OR REPLACE FUNCTION bump_backfill_attempts(url_hashes TEXT[], kind TEXT)
RETURNS INTEGER AS $$
DECLARE updated_count INTEGER;
BEGIN
  IF kind = 'image' THEN
    WITH updated AS (
      UPDATE articles
      SET image_backfill_attempts = image_backfill_attempts + 1,
          image_backfill_last_attempt_at = NOW()
      WHERE url_hash = ANY(url_hashes)
      RETURNING 1
    )
    SELECT count(*) INTO updated_count FROM updated;
  ELSIF kind = 'content' THEN
    WITH updated AS (
      UPDATE articles
      SET content_backfill_attempts = content_backfill_attempts + 1,
          content_backfill_last_attempt_at = NOW()
      WHERE url_hash = ANY(url_hashes)
      RETURNING 1
    )
    SELECT count(*) INTO updated_count FROM updated;
  ELSE
    RAISE EXCEPTION 'unknown backfill kind: %', kind;
  END IF;
  RETURN updated_count;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = public;

-- Service role only.
REVOKE ALL ON FUNCTION bump_backfill_attempts(TEXT[], TEXT) FROM PUBLIC;
REVOKE ALL ON FUNCTION bump_backfill_attempts(TEXT[], TEXT) FROM anon;
