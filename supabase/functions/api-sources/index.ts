import { corsHeaders, handleCors } from "../_shared/cors.ts";
import { CacheDurations, cacheHeaders } from "../_shared/cache.ts";
import { fetchFromSupabase, type ProxyConfig } from "../_shared/supabase-proxy.ts";

const config: ProxyConfig = {
  table: "sources",
  allowedParams: ["select", "id", "slug", "category_id", "is_active", "order"],
  defaultSelect: "id,name,slug,website_url,logo_url,category_id,is_active",
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
      }
    );
  }
});
