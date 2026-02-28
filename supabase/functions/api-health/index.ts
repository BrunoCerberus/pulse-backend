/**
 * Health Check API Endpoint
 *
 * Returns a simple health status indicating the API is operational.
 * Useful for uptime monitoring and load balancer health probes.
 *
 * ## Response
 * JSON object with fields:
 * - `status` - Always "ok" when the service is running
 * - `timestamp` - ISO 8601 timestamp of the response
 *
 * ## Caching
 * - Cache-Control: no-store (never cached)
 *
 * @module api-health
 */
import { corsHeaders, handleCors } from "../_shared/cors.ts";

export function handler(req: Request): Response {
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

  return new Response(
    JSON.stringify({
      status: "ok",
      timestamp: new Date().toISOString(),
    }),
    {
      status: 200,
      headers: {
        ...corsHeaders,
        "Cache-Control": "no-store",
        "Content-Type": "application/json",
      },
    },
  );
}

if (import.meta.main) {
  Deno.serve(handler);
}
