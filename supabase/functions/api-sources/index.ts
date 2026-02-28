/**
 * Sources API Endpoint
 *
 * Returns a list of RSS news sources configured in the system.
 * Sources represent individual news outlets (e.g., BBC News, TechCrunch).
 *
 * ## Query Parameters
 * - `category_id` - Filter by category UUID
 * - `is_active` - Filter by active status (e.g., `is_active=eq.true`)
 * - `slug` - Filter by source slug
 * - `order` - Sort order
 *
 * ## Response
 * JSON array of source objects with fields:
 * - `id` - UUID
 * - `name` - Display name (e.g., "BBC News")
 * - `slug` - URL-safe identifier (e.g., "bbc-news")
 * - `website_url` - Source website
 * - `logo_url` - Source logo image
 * - `category_id` - Associated category UUID
 * - `is_active` - Whether source is being fetched
 *
 * ## Caching
 * - Cache-Control: 1 hour (public, max-age=3600)
 *
 * @module api-sources
 */
import { corsHeaders, handleCors } from "../_shared/cors.ts";
import { CacheDurations, cacheHeaders } from "../_shared/cache.ts";
import { fetchFromSupabase, type ProxyConfig } from "../_shared/supabase-proxy.ts";
import { getCached, setCached } from "../_shared/memory-cache.ts";

const config: ProxyConfig = {
  table: "sources",
  allowedParams: ["id", "slug", "category_id", "language", "is_active", "order"],
  defaultSelect: "id,name,slug,website_url,logo_url,category_id,language,is_active",
};

const CACHE_TTL_MS = 30 * 60 * 1000; // 30 minutes

export async function handler(req: Request): Promise<Response> {
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
    // Check in-memory cache (key includes query string for param variations)
    const cacheKey = "sources:" + new URL(req.url).search;
    const cached = getCached(cacheKey);

    const data = cached ?? (await (async () => {
      const result = await fetchFromSupabase(req, config);
      if (result.status === 200) {
        setCached(cacheKey, result.data, CACHE_TTL_MS);
      }
      return result.data;
    })());

    return new Response(data, {
      status: 200,
      headers: {
        ...corsHeaders,
        ...cacheHeaders(CacheDurations.SOURCES),
        "Content-Type": "application/json",
      },
    });
  } catch (error) {
    console.error("Error fetching sources:", error);
    return new Response(
      JSON.stringify({ error: "Internal server error" }),
      {
        status: 500,
        headers: { ...corsHeaders, "Content-Type": "application/json" },
      }
    );
  }
}

if (import.meta.main) {
  Deno.serve(handler);
}
