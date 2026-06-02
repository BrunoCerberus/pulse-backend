# Database Schema

Documentation for the Pulse Backend PostgreSQL database hosted on Supabase.

## Tables

### categories

News categories for organizing articles and sources.

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID | Primary key, auto-generated |
| `name` | VARCHAR(100) | Display name (e.g., "Technology") |
| `slug` | VARCHAR(100) | URL-safe identifier (e.g., "technology") |
| `display_order` | INT | Sort order for UI display |
| `created_at` | TIMESTAMPTZ | Row creation timestamp |

**Constraints:**
- `name` and `slug` are unique

**Default Data:** 10 categories (World, Technology, Business, Sports, Entertainment, Science, Health, Politics, Podcasts, Videos)

---

### sources

RSS feed configurations for news sources.

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID | Primary key, auto-generated |
| `name` | VARCHAR(255) | Display name (e.g., "BBC News - Technology") |
| `slug` | VARCHAR(100) | Unique URL-safe identifier |
| `feed_url` | TEXT | RSS/Atom feed URL |
| `website_url` | TEXT | Source website homepage |
| `logo_url` | TEXT | Source logo image URL |
| `category_id` | UUID | FK to categories |
| `is_active` | BOOLEAN | Whether to fetch this source |
| `last_fetched_at` | TIMESTAMPTZ | Last successful fetch time |
| `created_at` | TIMESTAMPTZ | Row creation timestamp |
| `updated_at` | TIMESTAMPTZ | Last update timestamp |
| `language` | VARCHAR(5) | ISO 639-1 language code (default: 'en') |
| `fetch_interval_hours` | INT | Adaptive fetch frequency (migration 015) |
| `etag` | TEXT | Conditional-GET validator (migration 019) |
| `last_modified` | TEXT | Conditional-GET validator (migration 019) |
| `consecutive_failures` | INT | Circuit-breaker counter (migration 019) |
| `circuit_open_until` | TIMESTAMPTZ | Cool-off timestamp when circuit trips (migration 019) |

**Constraints:**
- `slug` is unique
- `category_id` references `categories(id)`

**Default Data:** 136 pre-configured sources across articles, podcasts, and videos in English, Portuguese, and Spanish

---

### articles

News articles fetched from RSS feeds.

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID | Primary key, auto-generated |
| `title` | TEXT | Article headline |
| `summary` | TEXT | Short description (from RSS) |
| `content` | TEXT | Full article text (extracted via go-readability) |
| `url` | TEXT | Original article URL |
| `url_hash` | VARCHAR(64) | SHA-256 hash of canonicalized URL for deduplication |
| `image_url` | TEXT | High-res image (preferring og:image) |
| `thumbnail_url` | TEXT | Original RSS thumbnail (often low-res) |
| `author` | VARCHAR(255) | Article author name |
| `source_id` | UUID | FK to sources |
| `category_id` | UUID | FK to categories (inherited from source) |
| `source_name` | TEXT | Denormalized from sources (migration 016) |
| `source_slug` | TEXT | Denormalized from sources (migration 016) |
| `category_name` | TEXT | Denormalized from categories (migration 016) |
| `category_slug` | TEXT | Denormalized from categories (migration 016) |
| `language` | VARCHAR(5) | ISO 639-1 language code (inherited from source) |
| `media_type` | VARCHAR(20) | Media type: 'podcast' or 'video' |
| `media_url` | TEXT | Direct URL to audio/video file |
| `media_duration` | INT | Duration in seconds (capped at 24h) |
| `media_mime_type` | VARCHAR(50) | MIME type (audio/mpeg, video/mp4, etc.) |
| `published_at` | TIMESTAMPTZ | Original publication date (clamped to ±10y from now) |
| `created_at` | TIMESTAMPTZ | When article was inserted |
| `search_vector` | tsvector | Auto-generated full-text search index |
| `image_backfill_attempts` | INT | Migration 018: retry counter |
| `image_backfill_last_attempt_at` | TIMESTAMPTZ | Migration 018: cooldown stamp |
| `content_backfill_attempts` | INT | Migration 018: retry counter |
| `content_backfill_last_attempt_at` | TIMESTAMPTZ | Migration 018: cooldown stamp |

**Constraints:**
- `url_hash` is unique (prevents duplicate articles)
- `source_id` references `sources(id)` with CASCADE delete
- `category_id` references `categories(id)`

**Generated Column:**
- `search_vector`: Automatically computed from `title` (weight A) and
  `summary` (weight B) for full-text search. (Migration 024 dropped
  `content` from the index to shrink GIN storage.)

---

### fetch_logs

Monitoring records for RSS fetch operations.

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID | Primary key, auto-generated |
| `started_at` | TIMESTAMPTZ | When fetch started |
| `completed_at` | TIMESTAMPTZ | When fetch completed |
| `sources_processed` | INT | Number of sources processed |
| `articles_fetched` | INT | Total articles found in feeds |
| `articles_inserted` | INT | New articles added to database |
| `articles_skipped` | INT | Duplicates skipped |
| `errors` | JSONB | Array of error messages |
| `status` | VARCHAR(20) | running, completed, or failed |

---

## Indexes

| Table | Index | Type | Purpose |
|-------|-------|------|---------|
| articles | `idx_articles_published_at` | B-tree DESC | Sort by date |
| articles | `idx_articles_source_id` | B-tree | Filter by source |
| articles | `idx_articles_category_id` | B-tree | Filter by category |
| articles | `idx_articles_url_hash` | B-tree | Deduplication lookups |
| articles | `idx_articles_created_at` | B-tree DESC | Cleanup queries |
| articles | `idx_articles_search` | GIN | Full-text search |
| articles | `idx_articles_category_published` | B-tree | Composite: category + date |
| articles | `idx_articles_image_backfill_candidates` | partial B-tree | Backfill candidate selection |
| articles | `idx_articles_content_backfill_candidates` | partial B-tree | Backfill candidate selection |
| fetch_logs | `idx_fetch_logs_started_at` | B-tree DESC | Recent logs |

Several indexes from migration 006 were dropped in migration 025 once they
showed `idx_scan = 0` over a multi-week observation window.

---

## Views

### articles_with_source

After migration 016, this is a denormalized simple SELECT (no JOINs).
Migration 027 rewrites it to project an explicit, public-safe column set
(no `url_hash`, no backfill state) and pins `security_invoker = on` so the
view respects the caller's RLS + column-level grants.

```sql
CREATE VIEW public.articles_with_source
WITH (security_invoker = on) AS
SELECT
    id, title, summary, content, url, image_url, thumbnail_url, author,
    published_at, created_at, language, source_id, category_id,
    source_name, source_slug, category_name, category_slug,
    media_type, media_url, media_duration, media_mime_type,
    search_vector
FROM public.articles;
```

`search_vector` is exposed because the iOS app filters via PostgREST's
`.textSearch("search_vector", ...)`; its content is derived from the
already-public title/summary so no extra leak.

### source_health

Per-source health snapshot. `security_invoker = on`.

Computes `circuit_open` (true when `circuit_open_until > NOW()`),
`most_recent_article_at`, and `articles_last_24h`.

**Granted to:** `service_role` only (migration 027). The
`api-source-health` Edge Function reads it with the service-role key
internally; anon callers hit the Edge Function, not the view directly.

---

## Functions

All SECURITY DEFINER functions use `SET search_path = ''` and fully
qualified references (`public.articles`, `pg_catalog.now()`, …). Each
includes an in-function `CURRENT_USER` check so a future REVOKE typo or
signature overload can't accidentally expose the write paths to anon.

### cleanup_old_articles(days_to_keep INT)

Removes articles older than the specified retention period.
SECURITY DEFINER, batched in 5,000-row chunks, `statement_timeout = '5min'`.

**Granted to:** `service_role` only.

### search_articles(search_query TEXT, result_limit INT)

Full-text search RPC. Returns an explicit projection (no `SETOF
articles`), capped at `result_limit = 100`. Rejects empty / whitespace /
> 200-char queries. SECURITY DEFINER (bypasses anon column grants on
`articles`), `statement_timeout = '3s'`.

**Granted to:** `anon, authenticated, service_role`.

```sql
SELECT * FROM search_articles('artificial intelligence', 10);
```

### batch_update_article_images(updates JSONB)

Single-round-trip image-URL updater. SECURITY DEFINER.

**Granted to:** `service_role` only.

### batch_update_article_content(updates JSONB)

Single-round-trip content updater (migration 026). SECURITY DEFINER.

**Granted to:** `service_role` only.

### bump_backfill_attempts(url_hashes TEXT[], kind TEXT)

Increments `image_backfill_attempts` / `content_backfill_attempts` and
stamps the corresponding `*_last_attempt_at`. Rejects arrays > 10000
entries. SECURITY DEFINER.

**Granted to:** `service_role` only.

### batch_update_source_fetch_state(updates JSONB)

Persists per-source `etag`, `last_modified`, `consecutive_failures`,
`circuit_open_until`, `last_fetched_at` in one round-trip
(migration 020). SECURITY DEFINER.

**Granted to:** `service_role` only.

### get_db_size_bytes()

Returns `pg_database_size(current_database())`. Used by
`api-source-health` to compute `quota_pct`.

**Granted to:** `service_role` only (migration 027 revoked anon access —
the Edge Function calls upstream with the service-role key).

---

## Row Level Security (RLS) + Column Grants

RLS is enabled on every table. After migration 027 the access model is:

| Table | anon SELECT | service_role |
|-------|-------------|--------------|
| `categories` | all rows, all columns | full |
| `sources` | rows with `is_active = true`, **column-level** (migration 034): `id, name, slug, website_url, logo_url, category_id, language, is_active` | full |
| `articles` | all rows, **column-level**: `id, title, summary, content, url, image_url, thumbnail_url, author, published_at, created_at, language, source_id, category_id, source_name, source_slug, category_name, category_slug, media_type, media_url, media_duration, media_mime_type, search_vector` | full |
| `fetch_logs` | nothing (defence-in-depth REVOKE) | full |

Backfill state (`*_backfill_attempts`, `*_backfill_last_attempt_at`) and
`url_hash` are reserved for the worker. On `sources`, the operational columns
(`feed_url`, `last_fetched_at`, `fetch_interval_hours`, `etag`, `last_modified`,
`consecutive_failures`, `circuit_open_until`, `max_content_length`) are likewise
service-role only (migration 034).

Writes are gated by the service-role key; there are no anon write policies.

---

## URL Canonicalization + Hash Deduplication

Articles are deduplicated by `url_hash`, the SHA-256 of a canonicalized
URL. Canonicalization (rss-worker, `internal/parser/parser.go`) drops the
fragment, lowercases scheme/host, and sorts query params before hashing,
so `https://X.com/a?b=2&a=1#frag` and `HTTPS://x.com/a?a=1&b=2` produce
the same hash.

```go
func HashURL(url string) string {
    hash := sha256.Sum256([]byte(url))
    return hex.EncodeToString(hash[:])
}
```

The unique constraint on `url_hash` lets Supabase reject duplicates with
a 409, which the worker handles gracefully.
