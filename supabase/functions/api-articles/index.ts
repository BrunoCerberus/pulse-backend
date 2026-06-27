/**
 * Articles API Endpoint
 *
 * Returns a paginated list of articles from the `articles_with_source` view.
 *
 * ## Query Parameters
 * - `limit` (default 100, max 100)
 * - `offset` (pagination)
 * - `source_slug` (e.g., `source_slug=eq.bbc-news`)
 * - `category_slug` (e.g., `category_slug=eq.technology`)
 * - `language` (e.g., `language=eq.en`)
 * - `media_type` (e.g., `media_type=eq.podcast`)
 * - `published_at` (e.g., `published_at=gte.2024-01-01`)
 * - `order` (only `published_at.asc|desc[.nullsfirst|.nullslast]`)
 * - `id` (e.g., `id=eq.<uuid>`)
 *
 * ## Response
 * JSON array of article objects with fields:
 * - `id`, `title`, `summary`, `url`, `image_url`
 * - `published_at`, `language`, `source_name`, `source_slug`, `category_name`, `category_slug`
 * - `media_type`, `media_url`, `media_duration`, `media_mime_type`
 *
 * ## Caching
 * - Cache-Control: 15 min fresh, 30 min stale-while-revalidate
 * - ETag/304 supported, but only for 200 OK upstream responses
 *
 * @module api-articles
 */
import { corsHeaders, handleCors } from "../_shared/cors.ts";
import { CacheDurations, cacheHeaders } from "../_shared/cache.ts";
import { checkConditionalRequest, generateETag } from "../_shared/etag.ts";
import {
  fetchFromSupabase,
  isLanguageFilter,
  type ProxyConfig,
  tooLong,
} from "../_shared/supabase-proxy.ts";

const config: ProxyConfig = {
  table: "articles_with_source",
  allowedParams: [
    "id",
    "source_slug",
    "category_slug",
    "language",
    "media_type",
    "order",
    "limit",
    "offset",
    "published_at",
  ],
  defaultSelect:
    "id,title,summary,url,image_url,published_at,language,source_name,source_slug,category_name,category_slug,media_type,media_url,media_duration,media_mime_type",
  defaultLimit: 100,
  maxLimit: 100,
  allowedOrderColumns: ["published_at"],
  paramValidators: {
    language: isLanguageFilter,
  },
};

export async function handler(req: Request): Promise<Response> {
  const corsResponse = handleCors(req);
  if (corsResponse) return corsResponse;

  if (req.method !== "GET") {
    return new Response(JSON.stringify({ error: "Method not allowed" }), {
      status: 405,
      headers: { ...corsHeaders, "Content-Type": "application/json" },
    });
  }

  const oversized = tooLong(req, corsHeaders);
  if (oversized) return oversized;

  try {
    const result = await fetchFromSupabase(req, config);

    // ETag/304 only on success — never cache or echo error bodies (which would
    // pin clients to a stale error state on `If-None-Match` replay, and may
    // leak PostgREST schema details). All non-200 upstream responses are
    // returned with a generic JSON error body.
    if (result.status !== 200) {
      return new Response(
        JSON.stringify({ error: "upstream error" }),
        {
          status: result.status,
          headers: { ...corsHeaders, "Content-Type": "application/json" },
        },
      );
    }

    const etag = await generateETag(result.data);
    const conditional = checkConditionalRequest(req, etag);
    if (conditional) {
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
      },
    );
  }
}

if (import.meta.main) {
  Deno.serve(handler);
}
