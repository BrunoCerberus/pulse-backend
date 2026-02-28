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

**Constraints:**
- `slug` is unique
- `category_id` references `categories(id)`

**Default Data:** 133 pre-configured sources across articles, podcasts, and videos in English, Portuguese, and Spanish

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
| `url_hash` | VARCHAR(64) | SHA256 hash of URL for deduplication |
| `image_url` | TEXT | High-res image (preferring og:image) |
| `thumbnail_url` | TEXT | Original RSS thumbnail (often low-res) |
| `author` | VARCHAR(255) | Article author name |
| `source_id` | UUID | FK to sources |
| `category_id` | UUID | FK to categories (inherited from source) |
| `language` | VARCHAR(5) | ISO 639-1 language code (inherited from source) |
| `media_type` | VARCHAR(20) | Media type: 'podcast' or 'video' |
| `media_url` | TEXT | Direct URL to audio/video file |
| `media_duration` | INT | Duration in seconds |
| `media_mime_type` | VARCHAR(50) | MIME type (audio/mpeg, video/mp4, etc.) |
| `published_at` | TIMESTAMPTZ | Original publication date |
| `created_at` | TIMESTAMPTZ | When article was inserted |
| `search_vector` | tsvector | Auto-generated full-text search index |

**Constraints:**
- `url_hash` is unique (prevents duplicate articles)
- `source_id` references `sources(id)` with CASCADE delete
- `category_id` references `categories(id)`

**Generated Column:**
- `search_vector`: Automatically computed from `title` (weight A), `summary` (weight B), and `content` (weight C) for full-text search

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
| articles | `idx_articles_source_published` | B-tree | Composite: source + date |
| articles | `idx_articles_language` | B-tree | Filter by language |
| articles | `idx_articles_media_type` | B-tree | Filter by media type |
| fetch_logs | `idx_fetch_logs_started_at` | B-tree DESC | Recent logs |

---

## Views

### articles_with_source

Joins articles with their source and category information for API responses.

```sql
SELECT
    a.id, a.title, a.summary, a.content, a.url,
    a.image_url, a.thumbnail_url, a.author,
    a.published_at, a.created_at,
    a.language,
    a.media_type, a.media_url, a.media_duration, a.media_mime_type,
    s.name as source_name,
    s.slug as source_slug,
    s.logo_url as source_logo_url,
    s.website_url as source_website_url,
    c.name as category_name,
    c.slug as category_slug
FROM articles a
LEFT JOIN sources s ON a.source_id = s.id
LEFT JOIN categories c ON a.category_id = c.id;
```

---

## Functions

### cleanup_old_articles(days_to_keep INT)

Removes articles older than the specified retention period.

**Parameters:**
- `days_to_keep`: Number of days to retain (default: 30)

**Returns:** Number of deleted rows

**Usage:**
```sql
SELECT cleanup_old_articles(30);
```

---

### search_articles(search_query TEXT, result_limit INT)

Full-text search across article titles and summaries.

**Parameters:**
- `search_query`: Search terms
- `result_limit`: Maximum results (default: 20)

**Returns:** Set of matching article rows, ranked by relevance

**Usage:**
```sql
SELECT * FROM search_articles('artificial intelligence', 10);
```

---

## Row Level Security (RLS)

RLS is enabled on all tables with the following policies:

### Public Read Access (anon key)
- `categories`: All rows readable
- `sources`: Only active sources readable (`is_active = true`)
- `articles`: All rows readable

### Service Role Write Access (service_role key)
- `fetch_logs`: ALL operations allowed

---

## URL Hash Deduplication

Articles are deduplicated using a SHA256 hash of the article URL:

```go
func HashURL(url string) string {
    hash := sha256.Sum256([]byte(url))
    return hex.EncodeToString(hash[:])
}
```

This allows the database to efficiently reject duplicate articles via the unique constraint on `url_hash`, returning a 409 Conflict that the worker handles gracefully.
