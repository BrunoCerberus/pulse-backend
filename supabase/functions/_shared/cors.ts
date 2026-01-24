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
 * - Allows requests from any origin (for public API)
 * - Permits GET and OPTIONS methods only (read-only API)
 * - Exposes ETag and Cache-Control for client-side caching
 */
export const corsHeaders = {
  "Access-Control-Allow-Origin": "*",
  "Access-Control-Allow-Methods": "GET, OPTIONS",
  "Access-Control-Allow-Headers":
    "authorization, x-client-info, apikey, content-type, if-none-match",
  "Access-Control-Expose-Headers": "etag, cache-control",
};

/**
 * Handles CORS preflight requests (OPTIONS method).
 *
 * Browsers send preflight requests before actual requests to check
 * if the server allows cross-origin access. This function returns
 * a 204 No Content response with CORS headers for preflight requests.
 *
 * @param req - The incoming HTTP request
 * @returns Response for OPTIONS requests, null for other methods
 *
 * @example
 * ```ts
 * const corsResponse = handleCors(req);
 * if (corsResponse) return corsResponse;
 * // Continue with actual request handling
 * ```
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
