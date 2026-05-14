/**
 * Health Check API Endpoint
 *
 * Minimal liveness probe. Returns `{ "status": "ok" }` when reachable.
 * The body intentionally contains no clock or version data so a probe
 * cannot be used to fingerprint the server.
 *
 * ## Caching
 * Cache-Control: no-store (never cached).
 *
 * @module api-health
 */
import { corsHeaders, handleCors } from "../_shared/cors.ts";

export function handler(req: Request): Response {
  const corsResponse = handleCors(req);
  if (corsResponse) return corsResponse;

  if (req.method !== "GET") {
    return new Response(JSON.stringify({ error: "Method not allowed" }), {
      status: 405,
      headers: { ...corsHeaders, "Content-Type": "application/json" },
    });
  }

  return new Response(
    JSON.stringify({ status: "ok" }),
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
