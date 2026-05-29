# API Reference

Complete documentation for the Pulse Backend REST API.

## Base URL

```
https://<project-id>.supabase.co/functions/v1
```

## Authentication

All endpoints are public — no authentication header required. The Edge
Functions authenticate to PostgREST on the client's behalf (`api-source-health`
uses the service role internally; the rest use the anon key).

## Hardened request handling

All proxy endpoints (`api-articles`, `api-sources`, `api-categories`,
`api-source-health`) enforce:

| Guard | Behavior |
|-------|----------|
| Column whitelist | The server always sends a fixed `select=` to PostgREST. Client `?select=` overrides are ignored — the response shape is the documented column set. |
| `limit` cap | `api-articles` clamps `limit` to 100. Negative / NaN / empty values fall back to the default. |
| `order` allow-list | Only the columns documented per endpoint can drive `order=`. Other columns silently fall back to the default. |
| URL length | Total request URI > 4096 chars → **414 Request URI too long**. |
| Per-value length | Each filter value > 256 chars is dropped (rejects giant `in.(...)` lists). |

These guards apply to traffic that goes through the Edge Functions. The iOS
app's direct PostgREST access (anon key on `articles_with_source`,
`sources`, `categories`) is bounded instead by the DB-layer column grants —
see [database-schema.md](./database-schema.md) for the exact column set.

---

## Endpoints

### GET /api-articles

Returns a paginated list of articles with source and category information.

- **Cache:** 15 minutes (with `stale-while-revalidate` for 30 minutes)
- **ETag:** Sent on 200 OK responses; send `If-None-Match` for a 304. Errors
  (4xx/5xx) are never ETagged, so a stale conditional request can't pin a
  client to an error state.

#### Query Parameters

| Parameter | Type | Description | Example |
|-----------|------|-------------|---------|
| `limit` | integer | Articles to return. Clamped to **[0, 100]**; default 100. | `limit=20` |
| `offset` | integer | Pagination offset (non-negative). | `offset=40` |
| `id` | string | Article ID, PostgREST syntax. | `id=eq.<uuid>` |
| `source_slug` | string | Filter by source slug. | `source_slug=eq.bbc-tech` |
| `category_slug` | string | Filter by category slug. | `category_slug=eq.technology` |
| `language` | string | ISO 639-1 language. | `language=eq.en` |
| `media_type` | string | `podcast` / `video`. | `media_type=eq.podcast` |
| `published_at` | string | Date filter, PostgREST operators (`gte`, `lte`, `eq`, …). | `published_at=gte.2024-01-01` |
| `order` | string | Sort order. **Only `published_at.<asc\|desc>[.nullsfirst\|.nullslast]`** is honored. | `order=published_at.desc` |

> `select` is *not* honored — the server always returns the canonical column
> set below.

#### Response

```json
[
  {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "title": "Article headline",
    "summary": "Brief description from RSS feed",
    "url": "https://example.com/article",
    "image_url": "https://example.com/image.jpg",
    "published_at": "2024-01-15T10:30:00Z",
    "language": "en",
    "source_name": "BBC News - Technology",
    "source_slug": "bbc-tech",
    "category_name": "Technology",
    "category_slug": "technology",
    "media_type": null,
    "media_url": null,
    "media_duration": null,
    "media_mime_type": null
  }
]
```

#### Response Headers

| Header | Description |
|--------|-------------|
| `ETag` | Hash of response data for conditional requests (200 only) |
| `Content-Range` | Pagination info (e.g., `0-19/1234`) |
| `Cache-Control` | `public, max-age=900, stale-while-revalidate=1800` |

#### Example

```bash
# Latest 10 technology articles, newest first
curl "https://<project>.supabase.co/functions/v1/api-articles?category_slug=eq.technology&limit=10&order=published_at.desc"

# With ETag for conditional request
curl -H "If-None-Match: \"a1b2c3d4e5f67890\"" \
  "https://<project>.supabase.co/functions/v1/api-articles?limit=20"
```

---

### GET /api-categories

Returns the list of news categories.

- **Cache:** 24 hours
- 1-hour in-memory cache at the Edge Function layer.

#### Query Parameters

| Parameter | Type | Description | Example |
|-----------|------|-------------|---------|
| `id` | string | UUID filter. | `id=eq.<uuid>` |
| `order` | string | Sort order. Allowed columns: `display_order`, `name`, `slug`. | `order=display_order.asc` |

#### Response

```json
[
  { "id": "...", "name": "Technology", "slug": "technology" },
  { "id": "...", "name": "Business",   "slug": "business" }
]
```

---

### GET /api-sources

Returns the list of RSS news sources.

- **Cache:** 1 hour
- 30-minute in-memory cache at the Edge Function layer.

#### Query Parameters

| Parameter | Type | Description | Example |
|-----------|------|-------------|---------|
| `id` | string | UUID filter. | `id=eq.<uuid>` |
| `slug` | string | Filter by source slug. | `slug=eq.bbc-tech` |
| `category_id` | string | Filter by category UUID. | `category_id=eq.<uuid>` |
| `language` | string | ISO 639-1 language. | `language=eq.en` |
| `is_active` | boolean | Filter by active status. | `is_active=eq.true` |
| `order` | string | Sort order. Allowed columns: `name`, `slug`, `language`. | `order=name.asc` |

#### Response

```json
[
  {
    "id": "550e8400-e29b-41d4-a716-446655440003",
    "name": "BBC News - Technology",
    "slug": "bbc-tech",
    "website_url": "https://www.bbc.com/news",
    "logo_url": null,
    "category_id": "550e8400-e29b-41d4-a716-446655440001",
    "language": "en",
    "is_active": true
  }
]
```

Operational columns (`feed_url`, `etag`, `last_modified`,
`consecutive_failures`, `circuit_open_until`, `last_fetched_at`,
`fetch_interval_hours`) are not exposed by this endpoint. Use direct
service-role PostgREST access if you need them.

---

### GET /api-search

Full-text search across articles via the `search_articles` RPC.

- **Cache:** 1 minute (private)

#### Query Parameters

| Parameter | Type | Description | Example |
|-----------|------|-------------|---------|
| `q` | string | Search query. **Max 200 characters.** Empty / whitespace / oversized queries return `[]` without a DB call. | `q=artificial intelligence` |
| `limit` | integer | Max results. Default 20, clamped to **[1, 100]**. | `limit=50` |

#### Response

Returns articles matching the search query, ranked by relevance. The
response shape matches `api-articles` plus `content`, `thumbnail_url`,
`author`, `source_id`, and `category_id` (see
[database-schema.md](./database-schema.md) for `search_articles` exact
columns).

Returns `[]` if the query is empty, whitespace, or > 200 characters.

#### Example

```bash
curl "https://<project>.supabase.co/functions/v1/api-search?q=artificial%20intelligence&limit=20"
```

---

### GET /api-health

Minimal liveness probe.

- **Cache:** `no-store`

#### Response

```json
{ "status": "ok" }
```

No clock/version data is returned, by design — a probe cannot be used to
fingerprint the server.

---

### GET /api-source-health

Aggregated per-source fetch health for the watchdog workflow and future
dashboards. Reads via the service-role key internally so the `source_health`
view can stay revoked from anon at the DB layer.

- **Cache:** 60s public
- Authentication: **none required** (anon-callable, public).

#### Query Parameters

| Parameter | Type | Description | Example |
|-----------|------|-------------|---------|
| `id` | string | Filter by source UUID. | `id=eq.<uuid>` |
| `slug` | string | Filter by source slug. | `slug=eq.bbc-tech` |
| `is_active` | boolean | Filter by active status. | `is_active=eq.true` |
| `order` | string | Sort order. Allowed: `name`, `slug`, `consecutive_failures`, `last_fetched_at`. | `order=consecutive_failures.desc` |

#### Response

```json
{
  "fetched_at": "2026-05-14T02:53:00.000Z",
  "database": {
    "size_bytes": 96468992,
    "size_pretty": "92.0 MB",
    "quota_pct": 18
  },
  "summary": {
    "total": 136,
    "active": 131,
    "circuit_open_count": 2,
    "high_failure_count": 5,
    "stale_count": 1
  },
  "sources": [
    {
      "id": "...", "name": "BBC News - Technology", "slug": "bbc-tech",
      "is_active": true,
      "consecutive_failures": 0, "circuit_open": false,
      "circuit_open_until": null,
      "last_fetched_at": "2026-05-14T00:45:47Z",
      "most_recent_article_at": "2026-05-14T00:30:12Z",
      "articles_last_24h": 12
    }
  ]
}
```

`database` is `null` if the DB-size RPC fails — the watchdog tolerates
`null` so transient size-check failures don't false-page.

---

## Direct REST API (Alternative)

Supabase auto-generates PostgREST endpoints under `/rest/v1` (requires the
`apikey` header). The Edge Functions above are preferred for the iOS app because
they add caching and request guards, but the raw REST surface is available:

```
# Base URL: https://<project-id>.supabase.co/rest/v1

GET /articles_with_source?order=published_at.desc&limit=20
GET /categories?order=display_order
GET /sources?is_active=eq.true
GET /rpc/search_articles?search_query=climate&result_limit=20
```

Direct access is bounded by the DB-layer column grants (anon sees only the safe
column set on `articles_with_source`, `sources`, `categories`) — see
[database-schema.md](./database-schema.md). Operational columns and the
`source_health` view require the service-role key.

---

## Error Responses

### 405 Method Not Allowed

Only GET is supported.

```json
{ "error": "Method not allowed" }
```

### 414 Request URI Too Long

Total query string > 4096 characters.

```json
{ "error": "Request URI too long" }
```

### 500 Internal Server Error

Check Supabase Edge Function logs for details.

```json
{ "error": "Internal server error" }
```

---

## CORS

All endpoints include CORS headers allowing requests from any origin:

```
Access-Control-Allow-Origin: *
Access-Control-Allow-Methods: GET, OPTIONS
Access-Control-Allow-Headers: authorization, x-client-info, apikey, content-type, if-none-match
Access-Control-Expose-Headers: etag, cache-control
```

Credentials are not allowed (no `Allow-Credentials`); the anon key is, by
design, public.

---

## Rate Limits

Rate limits are handled by Supabase infrastructure (Cloudflare in front)
plus the per-endpoint guards above. Free tier limits:

- 500,000 Edge Function invocations / month
- 2 million database rows read / month

See [Supabase pricing](https://supabase.com/pricing) for current limits.
