/**
 * Source Health API Endpoint
 *
 * Exposes per-source fetch health plus an aggregate summary so the watchdog
 * workflow (and future dashboards) can detect silently-degraded feeds.
 *
 * Reads from the `source_health` view (migration 020) — `security_invoker = on`
 * so it inherits the caller's RLS. The Edge Function calls upstream with
 * the service-role key (not anon), so the watchdog gets the full fleet
 * view while still being callable with `verify_jwt = false`.
 *
 * ## Response shape
 * ```
 * {
 *   "fetched_at": "ISO-8601",
 *   "database": { "size_bytes": N, "size_pretty": "X MB", "quota_pct": N } | null,
 *   "summary":  { "total": N, "active": N, "circuit_open_count": N,
 *                 "high_failure_count": N, "stale_count": N },
 *   "sources":  [SourceHealthRow, ...]
 * }
 * ```
 *
 * `high_failure_count` = sources with consecutive_failures ≥ 3 but circuit
 * not yet open. `stale_count` = active sources with no article in 7 days
 * and circuit still closed. `database` is `null` if the size RPC fails —
 * the watchdog tolerates `null` so transient size-check failures don't
 * false-page.
 *
 * ## Caching
 * Cache-Control: 60s public. Health doesn't need to be perfectly fresh.
 *
 * @module api-source-health
 */
import { corsHeaders, handleCors } from "../_shared/cors.ts";
import {
  buildCacheKey,
  buildProxyUrl,
  type ProxyConfig,
  tooLong,
} from "../_shared/supabase-proxy.ts";
import { getCached, setCached } from "../_shared/memory-cache.ts";

const config: ProxyConfig = {
  table: "source_health",
  allowedParams: ["id", "slug", "is_active", "order"],
  defaultSelect:
    "id,name,slug,is_active,consecutive_failures,circuit_open_until,circuit_open,last_fetched_at,most_recent_article_at,articles_last_24h",
  allowedOrderColumns: ["name", "slug", "consecutive_failures", "last_fetched_at"],
};

const CACHE_TTL_MS = 60 * 1000;
const CACHE_CONTROL = "public, max-age=60";
const HIGH_FAILURE_THRESHOLD = 3;
const STALE_MS = 7 * 24 * 60 * 60 * 1000;
const DEFAULT_QUOTA_BYTES = 524_288_000;

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

interface DatabaseSize {
  size_bytes: number;
  size_pretty: string;
  quota_pct: number;
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) {
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  }
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

function privilegedHeaders(): { url: string; headers: Record<string, string> } | null {
  // `source_health` is now revoked from anon (migration 027). The Edge
  // Function authenticates upstream as service_role so the watchdog can
  // get the full fleet view while api-source-health stays callable with
  // `verify_jwt = false`.
  const supabaseUrl = Deno.env.get("SUPABASE_URL");
  const serviceKey = Deno.env.get("SUPABASE_SERVICE_ROLE_KEY");
  if (!supabaseUrl || !serviceKey) return null;
  return {
    url: supabaseUrl,
    headers: {
      apikey: serviceKey,
      Authorization: `Bearer ${serviceKey}`,
      "Content-Type": "application/json",
      Prefer: "count=estimated",
    },
  };
}

export async function fetchDatabaseSize(): Promise<DatabaseSize | null> {
  const priv = privilegedHeaders();
  if (!priv) return null;

  const quotaRaw = Deno.env.get("SUPABASE_DB_QUOTA_BYTES");
  const quotaParsed = quotaRaw ? Number(quotaRaw) : DEFAULT_QUOTA_BYTES;
  const quotaBytes = Number.isFinite(quotaParsed) && quotaParsed > 0
    ? quotaParsed
    : DEFAULT_QUOTA_BYTES;

  try {
    const response = await fetch(`${priv.url}/rest/v1/rpc/get_db_size_bytes`, {
      method: "POST",
      headers: priv.headers,
      body: "{}",
    });
    if (response.status !== 200) return null;
    const sizeBytes = Number(await response.json());
    if (!Number.isFinite(sizeBytes) || sizeBytes < 0) return null;
    return {
      size_bytes: sizeBytes,
      size_pretty: formatBytes(sizeBytes),
      quota_pct: Math.round((sizeBytes / quotaBytes) * 100),
    };
  } catch {
    return null;
  }
}

export function summarize(rows: SourceHealthRow[]): HealthSummary {
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
      const recent = r.most_recent_article_at ? new Date(r.most_recent_article_at).getTime() : 0;
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

async function fetchSourceHealth(
  req: Request,
): Promise<{ status: number; data: string } | null> {
  const priv = privilegedHeaders();
  if (!priv) return null;
  const targetUrl = buildProxyUrl(req, config);
  const response = await fetch(targetUrl, {
    method: "GET",
    headers: priv.headers,
  });
  const data = await response.text();
  return { status: response.status, data };
}

async function buildPayload(req: Request): Promise<{ status: number; body: string }> {
  const [result, database] = await Promise.all([
    fetchSourceHealth(req),
    fetchDatabaseSize(),
  ]);
  if (result === null) {
    return {
      status: 500,
      body: JSON.stringify({ error: "Service configuration missing" }),
    };
  }
  if (result.status !== 200) {
    // Do NOT echo the upstream body. This query runs as service_role against
    // source_health (revoked from anon by migration 027), so its raw PostgREST
    // error text / SQLSTATE would disclose internals an anonymous caller can't
    // reach directly (e.g. a malformed `id`/`slug` triggers a 22P02 cast error).
    // Preserve the status code; return a generic body.
    return {
      status: result.status,
      body: JSON.stringify({ error: "upstream error" }),
    };
  }
  const rows: SourceHealthRow[] = JSON.parse(result.data);
  const payload = {
    fetched_at: new Date().toISOString(),
    database,
    summary: summarize(rows),
    sources: rows,
  };
  return { status: 200, body: JSON.stringify(payload) };
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

  const oversized = tooLong(req, corsHeaders);
  if (oversized) return oversized;

  try {
    const cacheKey = buildCacheKey("source-health", req, config);
    const cached = getCached(cacheKey);
    if (cached) {
      return new Response(cached, {
        status: 200,
        headers: {
          ...corsHeaders,
          "Cache-Control": CACHE_CONTROL,
          "Content-Type": "application/json",
        },
      });
    }
    const { status, body } = await buildPayload(req);
    if (status === 200) {
      setCached(cacheKey, body, CACHE_TTL_MS);
    }
    return new Response(body, {
      status,
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
