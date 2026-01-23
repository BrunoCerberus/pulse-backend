// Cache-Control header utilities

export const CacheDurations = {
  // Categories: static data, rarely changes
  CATEGORIES: "public, max-age=86400", // 24 hours

  // Sources: occasionally updated
  SOURCES: "public, max-age=3600", // 1 hour

  // Articles: updates every 15 minutes from RSS worker
  ARTICLES: "public, max-age=300, stale-while-revalidate=900", // 5 min fresh, 15 min stale

  // Search: query-dependent, private cache
  SEARCH: "private, max-age=60", // 1 minute
} as const;

export function cacheHeaders(
  cacheControl: string
): Record<string, string> {
  return {
    "Cache-Control": cacheControl,
  };
}
