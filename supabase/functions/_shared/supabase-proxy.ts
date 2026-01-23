// Proxy logic to forward requests to Supabase REST API

export interface ProxyConfig {
  table: string;
  allowedParams: string[];
  defaultSelect?: string;
}

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

export interface ProxyResult {
  data: string;
  status: number;
  contentRange: string | null;
}

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
