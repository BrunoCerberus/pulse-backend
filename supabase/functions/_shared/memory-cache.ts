/**
 * Bounded in-memory LRU TTL cache for Edge Functions.
 *
 * The previous implementation was an unbounded `Map`. Combined with the
 * proxy's incomplete cache-key canonicalization, an attacker could fill
 * the cache by sending requests with rotating junk query-string values.
 * This version caps total entries at MAX_ENTRIES (LRU eviction) on top
 * of the existing TTL.
 *
 * Keys must be canonicalized by the caller (use `buildCacheKey` from
 * supabase-proxy.ts) so that junk params don't produce unique keys.
 *
 * @module memory-cache
 */

interface Entry {
  data: string;
  expiry: number;
}

const MAX_ENTRIES = 1024;
const cache = new Map<string, Entry>();

function evictOldest(): void {
  // Map insertion order is preserved; the first key is the oldest entry.
  const first = cache.keys().next().value;
  if (first !== undefined) {
    cache.delete(first);
  }
}

export function getCached(key: string): string | null {
  const entry = cache.get(key);
  if (!entry) return null;
  if (Date.now() > entry.expiry) {
    cache.delete(key);
    return null;
  }
  // LRU bump: re-insert to mark this entry as most-recently-used.
  cache.delete(key);
  cache.set(key, entry);
  return entry.data;
}

export function setCached(key: string, data: string, ttlMs: number): void {
  if (cache.has(key)) {
    cache.delete(key);
  } else if (cache.size >= MAX_ENTRIES) {
    evictOldest();
  }
  cache.set(key, { data, expiry: Date.now() + ttlMs });
}

export function clearCache(): void {
  cache.clear();
}

export function cacheSize(): number {
  return cache.size;
}

export const MAX_CACHE_ENTRIES = MAX_ENTRIES;
