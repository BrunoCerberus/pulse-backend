/**
 * Search API Endpoint
 *
 * Full-text search across articles using PostgreSQL's tsvector indexing.
 * Calls the `search_articles` database function which performs ranked
 * full-text search on article titles, summaries, and content.
 *
 * ## Query Parameters
 * - `q` - Search query string (required for results)
 * - `limit` - Maximum results to return (default: 20, max: 100)
 *
 * ## Response
 * JSON array of matching article objects, ranked by relevance.
 * Returns empty array `[]` if query is empty or whitespace-only.
 *
 * ## Caching
 * - Cache-Control: 1 minute, private (user-specific results)
 *
 * @module api-search
 */
import { corsHeaders, handleCors } from "../_shared/cors.ts";
import { CacheDurations, cacheHeaders } from "../_shared/cache.ts";

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
    const supabaseUrl = Deno.env.get("SUPABASE_URL");
    const supabaseKey = Deno.env.get("SUPABASE_ANON_KEY");

    if (!supabaseUrl || !supabaseKey) {
      throw new Error("Supabase configuration missing");
    }

    const requestUrl = new URL(req.url);
    const query = requestUrl.searchParams.get("q") || "";
    const limit = Math.min(
      parseInt(requestUrl.searchParams.get("limit") || "20", 10),
      100
    );

    if (!query.trim()) {
      return new Response(JSON.stringify([]), {
        status: 200,
        headers: {
          ...corsHeaders,
          ...cacheHeaders(CacheDurations.SEARCH),
          "Content-Type": "application/json",
        },
      });
    }

    // Call the search_articles RPC function
    const rpcUrl = `${supabaseUrl}/rest/v1/rpc/search_articles`;
    const response = await fetch(rpcUrl, {
      method: "POST",
      headers: {
        apikey: supabaseKey,
        Authorization: `Bearer ${supabaseKey}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        search_query: query,
        result_limit: limit,
      }),
    });

    const data = await response.text();

    return new Response(data, {
      status: response.status,
      headers: {
        ...corsHeaders,
        ...cacheHeaders(CacheDurations.SEARCH),
        "Content-Type": "application/json",
      },
    });
  } catch (error) {
    console.error("Error searching articles:", error);
    return new Response(
      JSON.stringify({ error: "Internal server error" }),
      {
        status: 500,
        headers: { ...corsHeaders, "Content-Type": "application/json" },
      }
    );
  }
});
