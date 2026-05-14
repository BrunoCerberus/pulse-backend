/**
 * CORS (Cross-Origin Resource Sharing) utilities for Edge Functions.
 *
 * Enables the iOS app and web browsers to make requests to the API
 * by setting appropriate headers for cross-origin access.
 *
 * @module cors
 */

/**
 * Standard CORS headers for API responses.
 *
 * - Allows requests from any origin (this is a public read-only API)
 * - Permits GET and OPTIONS methods only
 * - Exposes ETag and Cache-Control for client-side caching
 *
 * Frozen so a downstream typo cannot mutate the shared object.
 */
export const corsHeaders: Readonly<Record<string, string>> = Object.freeze({
  "Access-Control-Allow-Origin": "*",
  "Access-Control-Allow-Methods": "GET, OPTIONS",
  "Access-Control-Allow-Headers":
    "authorization, x-client-info, apikey, content-type, if-none-match",
  "Access-Control-Expose-Headers": "etag, cache-control",
});

/**
 * Handles CORS preflight requests (OPTIONS method).
 *
 * Browsers send preflight requests before actual requests to check
 * if the server allows cross-origin access. This function returns
 * a 204 No Content response with CORS headers for preflight requests.
 *
 * @param req - The incoming HTTP request
 * @returns Response for OPTIONS requests, null for other methods
 */
export function handleCors(req: Request): Response | null {
  if (req.method === "OPTIONS") {
    return new Response(null, {
      status: 204,
      headers: corsHeaders,
    });
  }
  return null;
}
