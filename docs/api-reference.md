# API Reference

Complete documentation for the Pulse Backend REST API.

## Base URL

```
https://<project-id>.supabase.co/functions/v1
```

## Authentication

All endpoints are public and require no authentication. The Edge Functions use the Supabase anon key internally.

---

## Endpoints

### GET /api-articles

Returns a paginated list of articles with source and category information.

**Cache:** 5 minutes (with stale-while-revalidate for 15 minutes)
**ETag:** Supported (send `If-None-Match` header for 304 responses)

#### Query Parameters

| Parameter | Type | Description | Example |
|-----------|------|-------------|---------|
| `limit` | integer | Number of articles to return | `limit=20` |
| `offset` | integer | Pagination offset | `offset=40` |
| `source_slug` | string | Filter by source slug (PostgREST syntax) | `source_slug=eq.bbc-tech` |
| `category_slug` | string | Filter by category slug | `category_slug=eq.technology` |
| `order` | string | Sort order | `order=published_at.desc` |
| `published_at` | string | Date filter | `published_at=gte.2024-01-01` |
| `select` | string | Custom field selection | `select=id,title,url` |

#### Response

```json
[
  {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "title": "Article headline",
    "summary": "Brief description from RSS feed",
    "content": "Full article text extracted via readability",
    "url": "https://example.com/article",
    "image_url": "https://example.com/image.jpg",
    "published_at": "2024-01-15T10:30:00Z",
    "source_name": "BBC News - Technology",
    "source_slug": "bbc-tech",
    "category_name": "Technology",
    "category_slug": "technology"
  }
]
```

#### Response Headers

| Header | Description |
|--------|-------------|
| `ETag` | Hash of response data for conditional requests |
| `Content-Range` | Pagination info (e.g., `0-19/1234`) |
| `Cache-Control` | `public, max-age=300, stale-while-revalidate=900` |

#### Example

```bash
# Get latest 10 technology articles
curl "https://<project>.supabase.co/functions/v1/api-articles?category_slug=eq.technology&limit=10&order=published_at.desc"

# With ETag for conditional request
curl -H "If-None-Match: \"a1b2c3d4e5f67890\"" \
  "https://<project>.supabase.co/functions/v1/api-articles?limit=20"
```

---

### GET /api-categories

Returns the list of news categories.

**Cache:** 24 hours

#### Query Parameters

| Parameter | Type | Description | Example |
|-----------|------|-------------|---------|
| `order` | string | Sort order | `order=display_order.asc` |
| `select` | string | Custom field selection | `select=id,name,slug` |

#### Response

```json
[
  {
    "id": "550e8400-e29b-41d4-a716-446655440001",
    "name": "Technology",
    "slug": "technology"
  },
  {
    "id": "550e8400-e29b-41d4-a716-446655440002",
    "name": "Business",
    "slug": "business"
  }
]
```

#### Example

```bash
curl "https://<project>.supabase.co/functions/v1/api-categories?order=display_order.asc"
```

---

### GET /api-sources

Returns the list of RSS news sources.

**Cache:** 1 hour

#### Query Parameters

| Parameter | Type | Description | Example |
|-----------|------|-------------|---------|
| `category_id` | string | Filter by category UUID | `category_id=eq.<uuid>` |
| `is_active` | boolean | Filter by active status | `is_active=eq.true` |
| `slug` | string | Filter by source slug | `slug=eq.bbc-tech` |
| `order` | string | Sort order | `order=name.asc` |
| `select` | string | Custom field selection | `select=id,name,slug` |

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
    "is_active": true
  }
]
```

#### Example

```bash
# Get all active sources
curl "https://<project>.supabase.co/functions/v1/api-sources?is_active=eq.true"

# Get sources for a specific category
curl "https://<project>.supabase.co/functions/v1/api-sources?category_id=eq.<uuid>"
```

---

### GET /api-search

Full-text search across articles.

**Cache:** 1 minute (private)

#### Query Parameters

| Parameter | Type | Description | Example |
|-----------|------|-------------|---------|
| `q` | string | Search query (required) | `q=artificial intelligence` |
| `limit` | integer | Max results (default: 20, max: 100) | `limit=50` |

#### Response

Returns articles matching the search query, ranked by relevance.

```json
[
  {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "title": "AI breakthrough announced",
    "summary": "Researchers discover new...",
    "url": "https://example.com/article",
    "published_at": "2024-01-15T10:30:00Z"
  }
]
```

Returns empty array `[]` if query is empty.

#### Example

```bash
# Search for AI articles
curl "https://<project>.supabase.co/functions/v1/api-search?q=artificial%20intelligence&limit=20"
```

---

## Error Responses

### 405 Method Not Allowed

```json
{
  "error": "Method not allowed"
}
```

Only GET requests are supported.

### 500 Internal Server Error

```json
{
  "error": "Internal server error"
}
```

Check Supabase Edge Function logs for details.

---

## CORS

All endpoints include CORS headers allowing requests from any origin:

```
Access-Control-Allow-Origin: *
Access-Control-Allow-Methods: GET, OPTIONS
Access-Control-Allow-Headers: authorization, x-client-info, apikey, content-type, if-none-match
Access-Control-Expose-Headers: etag, cache-control
```

---

## Rate Limits

Rate limits are handled by Supabase infrastructure. Free tier limits:
- 500,000 Edge Function invocations/month
- 2 million database rows read/month

See [Supabase pricing](https://supabase.com/pricing) for current limits.
