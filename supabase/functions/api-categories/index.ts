/**
 * Categories API Endpoint
 *
 * Returns a list of news categories (e.g., Technology, Business, Sports).
 * Categories are static data that rarely changes.
 *
 * ## Query Parameters
 * - `order` - Sort order (e.g., `order=display_order.asc`)
 * - `select` - Custom field selection
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

const config: ProxyConfig = {
  table: "categories",
  allowedParams: ["select", "id", "order"],
  defaultSelect: "id,name,slug",
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

    return new Response(result.data, {
      status: result.status,
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
});
