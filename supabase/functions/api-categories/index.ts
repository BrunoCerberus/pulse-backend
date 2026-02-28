/**
 * Categories API Endpoint
 *
 * Returns a list of news categories (e.g., Technology, Business, Sports).
 * Categories are static data that rarely changes.
 *
 * ## Query Parameters
 * - `order` - Sort order (e.g., `order=display_order.asc`)
 *
 * ## Response
 * JSON array of category objects with fields:
 * - `id` - UUID
 * - `name` - Display name (e.g., "Technology")
 * - `slug` - URL-safe identifier (e.g., "technology")
 *
 * ## Caching
 * - Cache-Control: 24 hours (public, max-age=86400)
 *
 * @module api-categories
 */
import { corsHeaders, handleCors } from "../_shared/cors.ts";
import { CacheDurations, cacheHeaders } from "../_shared/cache.ts";
import { fetchFromSupabase, type ProxyConfig } from "../_shared/supabase-proxy.ts";
import { getCached, setCached } from "../_shared/memory-cache.ts";

const config: ProxyConfig = {
  table: "categories",
  allowedParams: ["id", "order"],
  defaultSelect: "id,name,slug",
};

const CACHE_TTL_MS = 60 * 60 * 1000; // 1 hour

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
    const cacheKey = "categories:" + new URL(req.url).search;
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
        ...cacheHeaders(CacheDurations.CATEGORIES),
        "Content-Type": "application/json",
      },
    });
  } catch (error) {
    console.error("Error fetching categories:", error);
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
