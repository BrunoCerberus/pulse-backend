-- Migration 014: Add RPC function for batch image updates
-- Allows updating image_url for multiple articles in a single call
-- instead of individual PATCH requests per article.

CREATE OR REPLACE FUNCTION batch_update_article_images(updates jsonb)
RETURNS integer AS $$
DECLARE updated_count integer;
BEGIN
  WITH updated AS (
    UPDATE articles a
    SET image_url = u.image_url
    FROM jsonb_to_recordset(updates) AS u(url_hash text, image_url text)
    WHERE a.url_hash = u.url_hash
      AND (a.image_url IS NULL OR a.image_url != u.image_url)
    RETURNING 1
  )
  SELECT count(*) INTO updated_count FROM updated;
  RETURN updated_count;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = public;

-- Only allow service role to call this function
REVOKE ALL ON FUNCTION batch_update_article_images(jsonb) FROM PUBLIC;
REVOKE ALL ON FUNCTION batch_update_article_images(jsonb) FROM anon;
