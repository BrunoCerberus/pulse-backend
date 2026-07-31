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
  isCacheableResult,
  isUpstreamSuccess,
  isUuidFilter,
  type ProxyConfig,
  tooLong,
} from "../_shared/supabase-proxy.ts";
import { getCached, setCached } from "../_shared/memory-cache.ts";

const config: ProxyConfig = {
  table: "categories",
  allowedParams: ["id", "order"],
  defaultSelect: "id,name,slug",
  allowedOrderColumns: ["display_order", "name", "slug"],
  paramValidators: {
    id: isUuidFilter,
  },
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
    const ok = {
      ...corsHeaders,
      ...cacheHeaders(CacheDurations.CATEGORIES),
      "Content-Type": "application/json",
    };

    if (cached !== null) {
      return new Response(cached, { status: 200, headers: ok });
    }

    const result = await fetchFromSupabase(req, config);

    // Mask unsuccessful upstream responses instead of echoing the raw
    // PostgREST body (SQLSTATE, column/table hints) under a hardcoded 200.
    if (!isUpstreamSuccess(result.status)) {
      return new Response(JSON.stringify({ error: "upstream error" }), {
        status: result.status,
        headers: { ...corsHeaders, "Content-Type": "application/json" },
      });
    }

    // Cache 200 only — see the note in api-sources: a cached 206 would replay
    // as a complete result without its `Content-Range`.
    if (result.status === 200 && isCacheableResult(result.data)) {
      setCached(cacheKey, result.data, CACHE_TTL_MS);
    }

    return new Response(result.data, { status: result.status, headers: ok });
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
