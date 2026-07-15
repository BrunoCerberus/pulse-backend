/**
 * Proxy utilities for forwarding requests to Supabase REST API.
 *
 * Hard-coded defaults:
 * - `select` is always forced to `defaultSelect`. The client cannot override
 *   it; this prevents column-leak attacks via `?select=*` (which would
 *   otherwise reach PostgREST and return every column of the underlying view).
 * - `limit` is clamped to `maxLimit` when the client provides one, or set
 *   to `defaultLimit` when absent.
 * - `order` is rejected unless it matches `<col>.<asc|desc>[.nullsfirst|.nullslast]`
 *   and `<col>` is in `allowedOrderColumns` (if configured).
 * - Empty param values are dropped (otherwise PostgREST treats `limit=` as
 *   "no limit" and ignores `defaultLimit`).
 * - Each forwarded value is capped at `maxValueLength` (default 256) to
 *   bound the cost of `in.(...)` lists and similar operator abuse.
 * - Total request URL is capped at MAX_QUERY_STRING_LENGTH to prevent
 *   pathological cache-key inflation and PostgREST request smuggling.
 *
 * @module supabase-proxy
 */

export interface ProxyConfig {
  /** Supabase table or view name to query. */
  table: string;

  /** Whitelist of query-param names the client may set. */
  allowedParams: string[];

  /**
   * PostgREST select clause. Always applied — the client's `select` is
   * never honored. This is the column-leak guard for the proxy.
   */
  defaultSelect: string;

  /** Applied when client omits `limit`. */
  defaultLimit?: number;

  /** Hard upper bound on `limit` when client provides one. */
  maxLimit?: number;

  /**
   * When `order` is in `allowedParams`, the column referenced must be one
   * of these. Other columns are rejected (silently dropped). Leave empty
   * to allow any column the underlying view exposes (NOT recommended —
   * lets clients sort by hidden columns and probe schema via 400s).
   */
  allowedOrderColumns?: string[];

  /**
   * Optional per-param value validators keyed by param name. When a param has
   * a validator and the client's value fails it, the value is DROPPED (same
   * as an invalid `order`) — it never reaches the upstream query or the cache
   * key. Values are full PostgREST filter expressions (e.g. `eq.en`,
   * `in.(a,b)`), so validators check the `<operator>.<value>` shape against
   * the column's domain (see isUuidFilter / isSlugFilter / isBooleanFilter).
   * Bounds the cache-key cardinality (and, for the service-role
   * api-source-health endpoint, the malformed-value DB round-trips).
   */
  paramValidators?: Record<string, (value: string) => boolean>;

  /** Per-value length cap. Defaults to 256. */
  maxValueLength?: number;
}

export interface ProxyResult {
  data: string;
  status: number;
  contentRange: string | null;
}

const MAX_QUERY_STRING_LENGTH = 4096;
const DEFAULT_MAX_VALUE_LENGTH = 256;
const ORDER_PATTERN = /^([a-z_][a-z0-9_]*)\.(asc|desc)(?:\.(nullsfirst|nullslast))?$/i;

/**
 * Rejects oversized request URIs before any DB work happens. Call this at
 * the top of every handler that uses the proxy.
 *
 * @param baseHeaders - additional headers to include on the 414 (typically
 *   `corsHeaders` so browser clients can read the response).
 */
export function tooLong(
  req: Request,
  baseHeaders: Record<string, string> = {},
): Response | null {
  const search = new URL(req.url).search;
  if (search.length > MAX_QUERY_STRING_LENGTH) {
    return new Response(
      JSON.stringify({ error: "Request URI too long" }),
      {
        status: 414,
        headers: { ...baseHeaders, "Content-Type": "application/json" },
      },
    );
  }
  return null;
}

export const MAX_QUERY_STRING_LEN = MAX_QUERY_STRING_LENGTH;

function isValidOrder(value: string, allowedColumns?: string[]): boolean {
  const m = ORDER_PATTERN.exec(value);
  if (!m) return false;
  if (allowedColumns && allowedColumns.length > 0 && !allowedColumns.includes(m[1])) {
    return false;
  }
  return true;
}

// --- Filter-value validators (for ProxyConfig.paramValidators) ----------------
// PostgREST filter values arrive as "<operator>.<value>" (e.g. "eq.en",
// "in.(en,pt)"). These helpers strip the operator and validate the value
// domain so malformed / high-cardinality junk is dropped before it can reach
// the upstream query or inflate the bounded in-memory cache key.

const FILTER_OP = /^(eq|neq|gt|gte|lt|lte|like|ilike|in|is|cs|cd)\.(.*)$/is;
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const SLUG_RE = /^[a-z0-9]+(?:-[a-z0-9]+)*$/i;
const ISO6391_RE = /^[a-z]{2}$/;

/** Returns the value portion of a PostgREST filter, or the raw string if it
 * carries no recognized operator prefix. */
function filterValueOf(raw: string): string {
  const m = FILTER_OP.exec(raw);
  return m ? m[2] : raw;
}

// F2: Reject compound PostgREST operators (in/and/or) on parameters without
// explicit validators. These operators let clients bypass per-value length
// caps — an `in.(v1,v2,...)` list can encode hundreds of items in a single
// 256-char value — and construct arbitrary filter expressions the proxy
// author never intended. Dropping them on unvalidated params keeps the proxy
// as a narrowing surface, not an oracle.
const COMPOUND_OP_RE = /^\s*in\.\(|^\s*(or|and)=/i;
function rejectsCompoundOperators(raw: string): boolean {
  return COMPOUND_OP_RE.test(raw);
}

/** Validates that an unvalidated filter value does not use compound operators. */
export function isSafeFilterValue(raw: string): boolean {
  return !rejectsCompoundOperators(raw);
}

/** Applies `ok` to each member of an `in.(a,b,c)` list, or to a single value. */
function everyFilterValue(raw: string, ok: (v: string) => boolean): boolean {
  const val = filterValueOf(raw);
  const inner = val.startsWith("(") && val.endsWith(")") ? val.slice(1, -1) : val;
  const items = inner.split(",").map((s) => s.trim()).filter((s) => s.length > 0);
  return items.length > 0 && items.every(ok);
}

/** Accepts only canonical UUID value(s) (e.g. `eq.<uuid>`, `in.(<uuid>,<uuid>)`). */
export function isUuidFilter(raw: string): boolean {
  return everyFilterValue(raw, (v) => UUID_RE.test(v));
}

/** Accepts only kebab-case slug value(s) up to 128 chars. */
export function isSlugFilter(raw: string): boolean {
  return everyFilterValue(raw, (v) => v.length <= 128 && SLUG_RE.test(v));
}

/** Accepts only boolean value(s): `eq.true` / `is.false` / `true` / `false`. */
export function isBooleanFilter(raw: string): boolean {
  return everyFilterValue(raw, (v) => v === "true" || v === "false");
}

/** Accepts only ISO 639-1 language code(s): `eq.en` / `in.(en,pt)` etc. */
export function isLanguageFilter(raw: string): boolean {
  return everyFilterValue(raw, (v) => ISO6391_RE.test(v));
}

/**
 * Reports whether an upstream JSON body is worth caching. Empty result sets
 * ("[]") are NOT cached: an attacker rotating non-matching filter values
 * (e.g. `?slug=eq.nonexistent-N`) would otherwise mint a unique cache key per
 * request and evict hot legitimate entries from the bounded in-memory LRU.
 * Zero-row queries are cheap to re-run; hot entries are not.
 */
export function isCacheableResult(data: string): boolean {
  const t = data.trim();
  return t !== "" && t !== "[]";
}

/**
 * Builds the upstream Supabase REST URL with sanitized, sorted params so
 * the same logical request always produces an identical URL (good for
 * caching and request fingerprinting).
 */
export function buildProxyUrl(req: Request, config: ProxyConfig): string {
  const supabaseUrl = Deno.env.get("SUPABASE_URL");
  if (!supabaseUrl) {
    throw new Error("SUPABASE_URL not configured");
  }

  const requestUrl = new URL(req.url);
  const targetUrl = new URL(`${supabaseUrl}/rest/v1/${config.table}`);
  const maxValueLength = config.maxValueLength ?? DEFAULT_MAX_VALUE_LENGTH;

  // Always force defaultSelect — never honor client `select`. This is the
  // single most important line in the file: it blocks `?select=*` from
  // reaching PostgREST and returning every column of the view.
  targetUrl.searchParams.set("select", config.defaultSelect);

  // Collect filtered/validated params into a sorted list so the final URL
  // (and any derived cache key) is canonical.
  const accepted: Array<[string, string]> = [];

  for (const param of config.allowedParams) {
    const raw = requestUrl.searchParams.get(param);
    if (raw === null || raw === "") continue;
    if (raw.length > maxValueLength) continue;

    if (param === "limit" || param === "offset") {
      const parsed = parseInt(raw, 10);
      if (!Number.isFinite(parsed) || parsed < 0) continue;
      let clamped = parsed;
      if (param === "limit" && config.maxLimit !== undefined) {
        clamped = Math.min(clamped, config.maxLimit);
      }
      accepted.push([param, String(clamped)]);
      continue;
    }

    if (param === "order") {
      if (!isValidOrder(raw, config.allowedOrderColumns)) continue;
      accepted.push([param, raw]);
      continue;
    }

    // Per-param value-domain validation. A failing value is dropped (like an
    // invalid `order`) so it never reaches the upstream query or cache key.
    const validate = config.paramValidators?.[param];
    if (validate && !validate(raw)) continue;

    // F2: On parameters without explicit validators, reject compound
    // PostgREST operators (in/and/or) that let clients bypass per-value
    // length caps and construct arbitrary filter expressions.
    if (!validate && !isSafeFilterValue(raw)) continue;

    accepted.push([param, raw]);
  }

  // Apply defaultLimit if no client-supplied limit survived validation.
  const haveLimit = accepted.some(([k]) => k === "limit");
  if (!haveLimit && config.defaultLimit !== undefined) {
    accepted.push(["limit", String(config.defaultLimit)]);
  }

  accepted.sort(([a], [b]) => a.localeCompare(b));
  for (const [k, v] of accepted) {
    targetUrl.searchParams.set(k, v);
  }

  return targetUrl.toString();
}

/**
 * Canonical cache key derived from the sanitized upstream URL. Two
 * requests that map to the same logical query share a cache entry; junk
 * params (which `buildProxyUrl` drops) do not inflate the cache.
 */
export function buildCacheKey(
  prefix: string,
  req: Request,
  config: ProxyConfig,
): string {
  const url = new URL(buildProxyUrl(req, config));
  return `${prefix}:${url.search}`;
}

export async function fetchFromSupabase(
  req: Request,
  config: ProxyConfig,
): Promise<ProxyResult> {
  const supabaseKey = Deno.env.get("SUPABASE_ANON_KEY");
  if (!supabaseKey) {
    throw new Error("SUPABASE_ANON_KEY not configured");
  }

  const targetUrl = buildProxyUrl(req, config);

  const response = await fetch(targetUrl, {
    method: "GET",
    headers: {
      apikey: supabaseKey,
      Authorization: `Bearer ${supabaseKey}`,
      "Content-Type": "application/json",
      Prefer: "count=estimated",
    },
  });

  const data = await response.text();

  return {
    data,
    status: response.status,
    contentRange: response.headers.get("content-range"),
  };
}
