/**
 * Categories API Endpoint
 *
 * Returns the list of news categories (rarely changes).
 *
 * ## Query Parameters
 * - `id` (UUID equality)
 * - `order` (only `display_order|name|slug.asc|desc`)
 *
 * ## Response
 * JSON array of `{ id, name, slug }`.
 *
 * ## Caching
 * Cache-Control: 24 hours (public). Plus 1 hour in-memory cache.
 *
 * @module api-categories
 */
import { corsHeaders, handleCors } from "../_shared/cors.ts";
import { CacheDurations, cacheHeaders } from "../_shared/cache.ts";
import {
  buildCacheKey,
  fetchFromSupabase,
  tooLong,
  type ProxyConfig,
} from "../_shared/supabase-proxy.ts";
import { getCached, setCached } from "../_shared/memory-cache.ts";

const config: ProxyConfig = {
  table: "categories",
  allowedParams: ["id", "order"],
  defaultSelect: "id,name,slug",
  allowedOrderColumns: ["display_order", "name", "slug"],
};

const CACHE_TTL_MS = 60 * 60 * 1000; // 1 hour

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
    const cacheKey = buildCacheKey("categories", req, config);
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
      },
    );
  }
}

if (import.meta.main) {
  Deno.serve(handler);
}
