/**
 * Cache-Control header utilities for Edge Functions.
 *
 * Provides consistent caching strategies across all API endpoints.
 * Cache durations are optimized based on data update frequency:
 * - Categories: Rarely change, long cache
 * - Sources: Occasional updates, medium cache
 * - Articles: Updated every 15min by RSS worker, short cache
 * - Search: User-specific, private short cache
 *
 * @module cache
 */

/**
 * Predefined Cache-Control header values for each endpoint type.
 *
 * These values balance freshness against CDN/client caching efficiency.
 * The `stale-while-revalidate` directive on articles allows serving
 * cached content while fetching updates in the background.
 */
export const CacheDurations = {
  /**
   * Categories: Static data that rarely changes.
   * Safe to cache for 24 hours.
   */
  CATEGORIES: "public, max-age=86400", // 24 hours

  /**
   * Sources: RSS source list, occasionally updated when adding new feeds.
   * 1 hour cache provides reasonable freshness.
   */
  SOURCES: "public, max-age=3600", // 1 hour

  /**
   * Articles: Main feed data, updated every 15 minutes by RSS worker.
   * 5 minute fresh cache with 15 minute stale-while-revalidate allows
   * CDNs to serve slightly stale content while fetching updates.
   */
  ARTICLES: "public, max-age=300, stale-while-revalidate=900", // 5 min fresh, 15 min stale

  /**
   * Search: Query results are user-specific and should not be shared.
   * Private cache with 1 minute TTL for repeated searches.
   */
  SEARCH: "private, max-age=60", // 1 minute
} as const;

/**
 * Creates a headers object with the specified Cache-Control value.
 *
 * @param cacheControl - The Cache-Control directive string
 * @returns Headers object to spread into Response headers
 *
 * @example
 * ```ts
 * return new Response(data, {
 *   headers: {
 *     ...corsHeaders,
 *     ...cacheHeaders(CacheDurations.ARTICLES),
 *   },
 * });
 * ```
 */
export function cacheHeaders(
  cacheControl: string
): Record<string, string> {
  return {
    "Cache-Control": cacheControl,
  };
}
