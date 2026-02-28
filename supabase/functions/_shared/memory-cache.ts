/**
 * Simple in-memory TTL cache for Edge Functions.
 *
 * Caches response strings with a time-to-live (TTL) to reduce
 * repeated Supabase REST API calls for slow-changing data like
 * categories and sources.
 *
 * @module memory-cache
 */

const cache = new Map<string, { data: string; expiry: number }>();

/**
 * Get a cached value by key. Returns null if not found or expired.
 */
export function getCached(key: string): string | null {
  const entry = cache.get(key);
  if (!entry || Date.now() > entry.expiry) {
    cache.delete(key);
    return null;
  }
  return entry.data;
}

/**
 * Set a cached value with a TTL in milliseconds.
 */
export function setCached(key: string, data: string, ttlMs: number): void {
  cache.set(key, { data, expiry: Date.now() + ttlMs });
}

/**
 * Clear all cached entries. Useful for testing.
 */
export function clearCache(): void {
  cache.clear();
}
