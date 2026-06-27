/**
 * Sources API Endpoint
 *
 * Returns the list of RSS news sources.
 *
 * ## Query Parameters
 * - `id` (UUID equality, e.g., `id=eq.<uuid>`)
 * - `slug` (e.g., `slug=eq.bbc-news`)
 * - `category_id` (UUID)
 * - `language` (e.g., `language=eq.en`)
 * - `is_active` (e.g., `is_active=eq.true`)
 * - `order` (only `name|slug|language.asc|desc`)
 *
 * ## Response
 * JSON array of source objects with fields: `id`, `name`, `slug`,
 * `website_url`, `logo_url`, `category_id`, `language`, `is_active`.
 *
 * ## Caching
 * - Cache-Control: 1 hour (public)
 * - Plus 30-minute in-memory cache to absorb burst load on the
 *   sa-east-1 Edge Function instance.
 *
 * @module api-sources
 */
import { corsHeaders, handleCors } from "../_shared/cors.ts";
import { CacheDurations, cacheHeaders } from "../_shared/cache.ts";
import {
  buildCacheKey,
  fetchFromSupabase,
  isBooleanFilter,
  isCacheableResult,
  isLanguageFilter,
  isUuidFilter,
  type ProxyConfig,
  tooLong,
} from "../_shared/supabase-proxy.ts";
import { getCached, setCached } from "../_shared/memory-cache.ts";

// Validators are scoped to params whose value domain is unambiguous (UUID,
// boolean, ISO 639-1 language). `slug` is intentionally left unvalidated so a
// publisher-defined slug can never be silently dropped; junk-value cache
// thrash on those is already neutralized by isCacheableResult (empty result
// sets aren't cached) below.
const config: ProxyConfig = {
  table: "sources",
  allowedParams: ["id", "slug", "category_id", "language", "is_active", "order"],
  defaultSelect: "id,name,slug,website_url,logo_url,category_id,language,is_active",
  allowedOrderColumns: ["name", "slug", "language"],
  paramValidators: {
    id: isUuidFilter,
    category_id: isUuidFilter,
    is_active: isBooleanFilter,
    language: isLanguageFilter,
  },
};

const CACHE_TTL_MS = 30 * 60 * 1000; // 30 minutes

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
    // Canonical cache key — junk/empty/oversized params never inflate cache.
    const cacheKey = buildCacheKey("sources", req, config);

    let data: string;
    let status = 200;

    const cached = getCached(cacheKey);
    if (cached !== null) {
      data = cached;
    } else {
      const result = await fetchFromSupabase(req, config);
      status = result.status;
      if (result.status === 200 && isCacheableResult(result.data)) {
        data = result.data;
        setCached(cacheKey, data, CACHE_TTL_MS);
      } else {
        // Mask any non-200 upstream response (PostgREST errors, gateway HTML)
        // with a generic JSON body. Only cache successful, non-empty results.
        data = result.status === 200 ? result.data : JSON.stringify({ error: "upstream error" });
      }
    }

    return new Response(data, {
      status,
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
      },
    );
  }
}

if (import.meta.main) {
  Deno.serve(handler);
}
