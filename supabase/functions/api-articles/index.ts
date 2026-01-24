/**
 * Articles API Endpoint
 *
 * Returns a paginated list of articles from the articles_with_source view,
 * which joins articles with their source and category information.
 *
 * ## Query Parameters
 * - `limit` - Number of articles to return (default: all)
 * - `offset` - Pagination offset
 * - `source_slug` - Filter by source (e.g., `source_slug=eq.bbc-news`)
 * - `category_slug` - Filter by category (e.g., `category_slug=eq.technology`)
 * - `order` - Sort order (e.g., `order=published_at.desc`)
 * - `published_at` - Date filter (e.g., `published_at=gte.2024-01-01`)
 * - `select` - Custom field selection
 *
 * ## Response
 * JSON array of article objects with fields:
 * - `id`, `title`, `summary`, `content`, `url`, `image_url`
 * - `published_at`, `source_name`, `source_slug`, `category_name`, `category_slug`
 *
 * ## Caching
 * - Cache-Control: 5 minutes fresh, 15 minutes stale-while-revalidate
 * - ETag support for conditional requests (304 Not Modified)
 *
 * @module api-articles
 */
import { corsHeaders, handleCors } from "../_shared/cors.ts";
import { CacheDurations, cacheHeaders } from "../_shared/cache.ts";
import { generateETag, checkConditionalRequest } from "../_shared/etag.ts";
import { fetchFromSupabase, type ProxyConfig } from "../_shared/supabase-proxy.ts";

const config: ProxyConfig = {
  table: "articles_with_source",
  allowedParams: [
    "select",
    "id",
    "source_slug",
    "category_slug",
    "order",
    "limit",
    "offset",
    "published_at",
  ],
  defaultSelect:
    "id,title,summary,content,url,image_url,published_at,source_name,source_slug,category_name,category_slug",
};

Deno.serve(async (req: Request) => {
  // Handle CORS preflight
  const corsResponse = handleCors(req);
  if (corsResponse) return corsResponse;

  // Only allow GET
  if (req.method !== "GET") {
    return new Response(JSON.stringify({ error: "Method not allowed" }), {
      status: 405,
      headers: { ...corsHeaders, "Content-Type": "application/json" },
    });
  }

  try {
    const result = await fetchFromSupabase(req, config);

    // Generate ETag from response data
    const etag = await generateETag(result.data);

    // Check for conditional request (304 Not Modified)
    const conditionalResponse = checkConditionalRequest(req, etag);
    if (conditionalResponse) {
      return new Response(null, {
        status: 304,
        headers: {
          ...corsHeaders,
          ...cacheHeaders(CacheDurations.ARTICLES),
          ETag: etag,
        },
      });
    }

    const headers: Record<string, string> = {
      ...corsHeaders,
      ...cacheHeaders(CacheDurations.ARTICLES),
      "Content-Type": "application/json",
      ETag: etag,
    };

    // Include content-range if present (for pagination)
    if (result.contentRange) {
      headers["Content-Range"] = result.contentRange;
    }

    return new Response(result.data, {
      status: result.status,
      headers,
    });
  } catch (error) {
    console.error("Error fetching articles:", error);
    return new Response(
      JSON.stringify({ error: "Internal server error" }),
      {
        status: 500,
        headers: { ...corsHeaders, "Content-Type": "application/json" },
      }
    );
  }
});
