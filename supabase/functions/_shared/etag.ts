/**
 * ETag generation and conditional request handling for Edge Functions.
 *
 * ETags enable HTTP conditional requests (If-None-Match), allowing clients
 * to receive 304 Not Modified responses when data hasn't changed. This
 * reduces bandwidth and improves perceived performance for the iOS app.
 *
 * @module etag
 */

/**
 * Generates an ETag from response data using SHA-256 hashing.
 *
 * The ETag is a quoted string containing the first 16 hex characters
 * of the SHA-256 hash. This provides sufficient uniqueness while
 * keeping the header size small.
 *
 * @param data - The response body string to hash
 * @returns A quoted ETag string (e.g., `"a1b2c3d4e5f67890"`)
 *
 * @example
 * ```ts
 * const etag = await generateETag(JSON.stringify(articles));
 * // etag = '"a1b2c3d4e5f67890"'
 * ```
 */
export async function generateETag(data: string): Promise<string> {
  const encoder = new TextEncoder();
  const dataBuffer = encoder.encode(data);
  const hashBuffer = await crypto.subtle.digest("SHA-256", dataBuffer);
  const hashArray = Array.from(new Uint8Array(hashBuffer));
  const hashHex = hashArray.map((b) => b.toString(16).padStart(2, "0")).join("");
  return `"${hashHex.slice(0, 16)}"`;
}

/**
 * Checks if the request includes a matching If-None-Match header.
 *
 * If the client's cached ETag matches the current data's ETag,
 * returns a 304 Not Modified response. Otherwise returns null
 * to indicate the full response should be sent.
 *
 * @param req - The incoming HTTP request
 * @param etag - The current ETag for the response data
 * @returns 304 Response if ETags match, null otherwise
 *
 * @example
 * ```ts
 * const etag = await generateETag(data);
 * const notModified = checkConditionalRequest(req, etag);
 * if (notModified) return notModified;
 * // Send full response with ETag header
 * ```
 */
export function checkConditionalRequest(
  req: Request,
  etag: string
): Response | null {
  const ifNoneMatch = req.headers.get("if-none-match");
  if (ifNoneMatch && ifNoneMatch === etag) {
    return new Response(null, {
      status: 304,
      headers: {
        ETag: etag,
      },
    });
  }
  return null;
}
