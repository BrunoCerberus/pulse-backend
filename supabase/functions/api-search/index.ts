/**
 * Search API Endpoint
 *
 * Full-text search across articles via PostgreSQL's tsvector indexing.
 * Calls the `search_articles` RPC which performs ranked full-text search.
 *
 * ## Query Parameters
 * - `q` - search query (required; length-capped at MAX_QUERY_LEN)
 * - `limit` - results to return (default 20, max 100, min 1)
 * - `offset` - results to skip, for pagination (default 0)
 * - `language` - ISO 639-1 content language (e.g. `en`); omitted or
 *   malformed means "all languages"
 *
 * ## Response
 * JSON array of matching articles ranked by relevance. Empty array for
 * empty/whitespace/oversized queries.
 *
 * ## Caching
 * Cache-Control: 1 minute, public (same response for all callers since
 * `verify_jwt = false`).
 *
 * @module api-search
 */
import { corsHeaders, handleCors } from "../_shared/cors.ts";
import { CacheDurations, cacheHeaders } from "../_shared/cache.ts";

export const MAX_QUERY_LEN = 200;
const DEFAULT_LIMIT = 20;
const MAX_LIMIT = 100;
const MIN_LIMIT = 1;

const ISO6391_RE = /^[a-z]{2}$/;

function parseLimit(raw: string | null): number {
  const parsed = parseInt(raw ?? "", 10);
  if (!Number.isFinite(parsed)) return DEFAULT_LIMIT;
  return Math.min(Math.max(parsed, MIN_LIMIT), MAX_LIMIT);
}

function parseOffset(raw: string | null): number {
  const parsed = parseInt(raw ?? "", 10);
  if (!Number.isFinite(parsed) || parsed < 0) return 0;
  return parsed;
}

/**
 * Content-language filter. Anything that isn't a bare ISO 639-1 code is
 * dropped to `null` (= all languages) rather than rejected: the value also
 * lands in the response cache key, so free-form junk here would let a caller
 * mint unlimited distinct entries.
 */
function parseLanguage(raw: string | null): string | null {
  return raw !== null && ISO6391_RE.test(raw) ? raw : null;
}

export async function handler(req: Request): Promise<Response> {
  const corsResponse = handleCors(req);
  if (corsResponse) return corsResponse;

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
    const query = (requestUrl.searchParams.get("q") || "").trim();
    const limit = parseLimit(requestUrl.searchParams.get("limit"));
    const offset = parseOffset(requestUrl.searchParams.get("offset"));
    const language = parseLanguage(requestUrl.searchParams.get("language"));

    // Reject empty AND oversized queries with an empty result set — same
    // shape as the iOS app already handles, no error path needed.
    if (!query || query.length > MAX_QUERY_LEN) {
      return new Response(JSON.stringify([]), {
        status: 200,
        headers: {
          ...corsHeaders,
          ...cacheHeaders(CacheDurations.SEARCH),
          "Content-Type": "application/json",
        },
      });
    }

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
        search_language: language,
        result_offset: offset,
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
      },
    );
  }
}

if (import.meta.main) {
  Deno.serve(handler);
}
