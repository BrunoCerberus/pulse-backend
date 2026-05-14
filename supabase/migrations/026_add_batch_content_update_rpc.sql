-- Migration 026: Add RPC function for batch article-content updates
--
-- Mirrors `batch_update_article_images` (migration 014) so the content
-- backfill can write extracted text via a single round-trip per chunk
-- instead of one PATCH per article. The single-row path was the largest
-- remaining write hot-spot once images were batched.

CREATE OR REPLACE FUNCTION batch_update_article_content(updates jsonb)
RETURNS integer AS $$
DECLARE updated_count integer;
BEGIN
  WITH updated AS (
    UPDATE articles a
    SET content = u.content
    FROM jsonb_to_recordset(updates) AS u(url_hash text, content text)
    WHERE a.url_hash = u.url_hash
      AND (a.content IS NULL OR a.content = '' OR a.content != u.content)
    RETURNING 1
  )
  SELECT count(*) INTO updated_count FROM updated;
  RETURN updated_count;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = public;

-- Service role only.
REVOKE ALL ON FUNCTION batch_update_article_content(jsonb) FROM PUBLIC;
REVOKE ALL ON FUNCTION batch_update_article_content(jsonb) FROM anon;
