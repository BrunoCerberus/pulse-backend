/**
 * Source Health API Endpoint
 *
 * Exposes per-source fetch health plus an aggregate summary so the watchdog
 * workflow (and future dashboards) can detect silently-degraded feeds without
 * eyeballing the `fetch_logs` table.
 *
 * Reads from the `source_health` view (migration 020) which derives:
 * - circuit_open (whether the circuit is currently tripped)
 * - most_recent_article_at (content freshness signal)
 * - articles_last_24h (rate-of-ingestion signal)
 *
 * ## Response
 * ```json
 * {
 *   "fetched_at": "2026-04-21T12:00:00.000Z",
 *   "summary": {
 *     "total": 133,
 *     "active": 131,
 *     "circuit_open_count": 2,
 *     "high_failure_count": 5,
 *     "stale_count": 1
 *   },
 *   "sources": [...]
 * }
 * ```
 *
 * `high_failure_count` = sources with consecutive_failures ≥ 3 but circuit
 * not yet open. `stale_count` = active sources with no article in 48h and
 * circuit still closed (silent degradation — the usual failure mode this
 * endpoint exists to catch).
 *
 * ## Caching
 * - Cache-Control: 60s public. Health doesn't need to be perfectly fresh;
 *   this rate still lets the watchdog (6h cron) see the current state.
 *
 * @module api-source-health
 */
import { corsHeaders, handleCors } from "../_shared/cors.ts";
import { fetchFromSupabase, type ProxyConfig } from "../_shared/supabase-proxy.ts";
import { getCached, setCached } from "../_shared/memory-cache.ts";

const config: ProxyConfig = {
  table: "source_health",
  allowedParams: ["id", "slug", "is_active", "order"],
  defaultSelect:
    "id,name,slug,is_active,consecutive_failures,circuit_open_until,circuit_open,last_fetched_at,most_recent_article_at,articles_last_24h",
};

const CACHE_TTL_MS = 60 * 1000; // 60s
const CACHE_CONTROL = "public, max-age=60";
const HIGH_FAILURE_THRESHOLD = 3; // warn before circuit opens (default trip at 5)
const STALE_MS = 48 * 60 * 60 * 1000;

interface SourceHealthRow {
  id: string;
  name: string;
  slug: string;
  is_active: boolean;
  consecutive_failures: number;
  circuit_open: boolean;
  circuit_open_until: string | null;
  last_fetched_at: string | null;
  most_recent_article_at: string | null;
  articles_last_24h: number;
}

interface HealthSummary {
  total: number;
  active: number;
  circuit_open_count: number;
  high_failure_count: number;
  stale_count: number;
}

function summarize(rows: SourceHealthRow[]): HealthSummary {
  const staleCutoff = Date.now() - STALE_MS;
  let active = 0;
  let circuitOpenCount = 0;
  let highFailureCount = 0;
  let staleCount = 0;

  for (const r of rows) {
    if (r.is_active) active++;
    if (r.circuit_open) circuitOpenCount++;
    if (!r.circuit_open && r.consecutive_failures >= HIGH_FAILURE_THRESHOLD) {
      highFailureCount++;
    }
    if (r.is_active && !r.circuit_open) {
      const recent = r.most_recent_article_at
        ? new Date(r.most_recent_article_at).getTime()
        : 0;
      if (recent < staleCutoff) staleCount++;
    }
  }

  return {
    total: rows.length,
    active,
    circuit_open_count: circuitOpenCount,
    high_failure_count: highFailureCount,
    stale_count: staleCount,
  };
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
    const cacheKey = "source-health:" + new URL(req.url).search;
    const cached = getCached(cacheKey);

    const body = cached ?? (await (async () => {
      const result = await fetchFromSupabase(req, config);
      if (result.status !== 200) {
        // Don't cache failures — let the next request retry.
        return result.data;
      }
      const rows: SourceHealthRow[] = JSON.parse(result.data);
      const payload = {
        fetched_at: new Date().toISOString(),
        summary: summarize(rows),
        sources: rows,
      };
      const serialized = JSON.stringify(payload);
      setCached(cacheKey, serialized, CACHE_TTL_MS);
      return serialized;
    })());

    return new Response(body, {
      status: 200,
      headers: {
        ...corsHeaders,
        "Cache-Control": CACHE_CONTROL,
        "Content-Type": "application/json",
      },
    });
  } catch (error) {
    console.error("Error fetching source health:", error);
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
