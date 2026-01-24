/**
 * Proxy utilities for forwarding requests to Supabase REST API.
 *
 * These functions provide a secure proxy layer that:
 * - Whitelists allowed query parameters to prevent injection
 * - Adds authentication headers automatically
 * - Handles response parsing and error propagation
 *
 * @module supabase-proxy
 */

/**
 * Configuration for proxying requests to a Supabase table or view.
 */
export interface ProxyConfig {
  /** The Supabase table or view name to query (e.g., "articles_with_source") */
  table: string;

  /** Query parameters to forward from the client request (whitelist for security) */
  allowedParams: string[];

  /** Default PostgREST select clause if not specified by client */
  defaultSelect?: string;
}

/**
 * Result of a proxied Supabase request.
 */
export interface ProxyResult {
  /** Raw JSON response body as string */
  data: string;

  /** HTTP status code from Supabase */
  status: number;

  /** Content-Range header for pagination (e.g., "0-9/100") */
  contentRange: string | null;
}

/**
 * Builds a Supabase REST API URL from the incoming request and config.
 *
 * Only whitelisted query parameters are forwarded to prevent
 * unauthorized access to sensitive data or injection attacks.
 *
 * @param req - The incoming HTTP request with query parameters
 * @param config - Proxy configuration specifying table and allowed params
 * @returns Full Supabase REST API URL with filtered query params
 * @throws Error if SUPABASE_URL environment variable is not set
 *
 * @example
 * ```ts
 * const url = buildProxyUrl(req, {
 *   table: "articles_with_source",
 *   allowedParams: ["limit", "offset", "category_slug"],
 * });
 * // url = "https://xxx.supabase.co/rest/v1/articles_with_source?limit=10"
 * ```
 */
export function buildProxyUrl(
  req: Request,
  config: ProxyConfig
): string {
  const supabaseUrl = Deno.env.get("SUPABASE_URL");
  if (!supabaseUrl) {
    throw new Error("SUPABASE_URL not configured");
  }

  const requestUrl = new URL(req.url);
  const targetUrl = new URL(`${supabaseUrl}/rest/v1/${config.table}`);

  // Add default select if provided
  if (config.defaultSelect && !requestUrl.searchParams.has("select")) {
    targetUrl.searchParams.set("select", config.defaultSelect);
  }

  // Whitelist and copy allowed query params
  for (const param of config.allowedParams) {
    const value = requestUrl.searchParams.get(param);
    if (value !== null) {
      targetUrl.searchParams.set(param, value);
    }
  }

  return targetUrl.toString();
}

/**
 * Proxies a request to Supabase without exact count header.
 *
 * @deprecated Use fetchFromSupabase instead for pagination support
 * @param req - The incoming HTTP request
 * @param config - Proxy configuration
 * @returns Raw Response-like object (not a proper Response)
 */
export async function proxyToSupabase(
  req: Request,
  config: ProxyConfig
): Promise<Response> {
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
    },
  });

  const data = await response.text();

  return {
    data,
    status: response.status,
    contentRange: response.headers.get("content-range"),
  } as unknown as Response;
}

/**
 * Fetches data from Supabase with exact count for pagination support.
 *
 * Adds `Prefer: count=exact` header to get total count in Content-Range
 * header, enabling pagination UIs to show total results.
 *
 * @param req - The incoming HTTP request with query parameters
 * @param config - Proxy configuration specifying table and allowed params
 * @returns ProxyResult with data, status, and optional content-range
 * @throws Error if SUPABASE_ANON_KEY environment variable is not set
 *
 * @example
 * ```ts
 * const result = await fetchFromSupabase(req, {
 *   table: "articles_with_source",
 *   allowedParams: ["limit", "offset", "category_slug"],
 *   defaultSelect: "id,title,summary,url",
 * });
 *
 * if (result.status !== 200) {
 *   return new Response(result.data, { status: result.status });
 * }
 *
 * // result.contentRange = "0-9/1234" for pagination
 * ```
 */
export async function fetchFromSupabase(
  req: Request,
  config: ProxyConfig
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
      Prefer: "count=exact",
    },
  });

  const data = await response.text();

  return {
    data,
    status: response.status,
    contentRange: response.headers.get("content-range"),
  };
}
