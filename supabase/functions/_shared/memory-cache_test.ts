import { assertEquals } from "https://deno.land/std@0.208.0/assert/assert_equals.ts";
import { cacheSize, clearCache, getCached, MAX_CACHE_ENTRIES, setCached } from "./memory-cache.ts";

Deno.test("getCached returns null for missing key", () => {
  clearCache();
  assertEquals(getCached("nonexistent"), null);
});

Deno.test("setCached and getCached round-trip", () => {
  clearCache();
  setCached("key1", '{"data": true}', 60000);
  assertEquals(getCached("key1"), '{"data": true}');
});

Deno.test("getCached returns null for expired entry", () => {
  clearCache();
  setCached("expired", "old", -1);
  assertEquals(getCached("expired"), null);
});

Deno.test("clearCache removes all entries", () => {
  clearCache();
  setCached("a", "1", 60000);
  setCached("b", "2", 60000);
  clearCache();
  assertEquals(getCached("a"), null);
  assertEquals(getCached("b"), null);
  assertEquals(cacheSize(), 0);
});

Deno.test("setCached overwrites existing entry", () => {
  clearCache();
  setCached("key", "old", 60000);
  setCached("key", "new", 60000);
  assertEquals(getCached("key"), "new");
  assertEquals(cacheSize(), 1);
});

Deno.test("cache evicts oldest entry when at capacity", () => {
  clearCache();
  // Fill to capacity.
  for (let i = 0; i < MAX_CACHE_ENTRIES; i++) {
    setCached(`k${i}`, `v${i}`, 60_000);
  }
  assertEquals(cacheSize(), MAX_CACHE_ENTRIES);
  // Insert one more — oldest (k0) is evicted.
  setCached("overflow", "new", 60_000);
  assertEquals(cacheSize(), MAX_CACHE_ENTRIES);
  assertEquals(getCached("k0"), null);
  assertEquals(getCached("overflow"), "new");
});

Deno.test("getCached promotes entry to most-recently-used (LRU)", () => {
  clearCache();
  // Fill to capacity.
  for (let i = 0; i < MAX_CACHE_ENTRIES; i++) {
    setCached(`k${i}`, `v${i}`, 60_000);
  }
  // Read the oldest entry — it should become the newest.
  assertEquals(getCached("k0"), "v0");
  // Now insert a new entry — k1 should be the one evicted, not k0.
  setCached("new", "n", 60_000);
  assertEquals(getCached("k0"), "v0");
  assertEquals(getCached("k1"), null);
});

Deno.test("getCached on expired entry returns null without LRU side effect", () => {
  clearCache();
  setCached("a", "1", -1);
  setCached("b", "2", 60_000);
  assertEquals(getCached("a"), null);
  assertEquals(getCached("b"), "2");
  assertEquals(cacheSize(), 1);
});
