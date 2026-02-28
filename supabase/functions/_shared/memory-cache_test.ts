import { assertEquals } from "https://deno.land/std@0.208.0/assert/assert_equals.ts";
import { getCached, setCached, clearCache } from "./memory-cache.ts";

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
  // Set with TTL of 0ms (already expired)
  setCached("expired", "old", 0);
  assertEquals(getCached("expired"), null);
});

Deno.test("clearCache removes all entries", () => {
  clearCache();
  setCached("a", "1", 60000);
  setCached("b", "2", 60000);
  clearCache();
  assertEquals(getCached("a"), null);
  assertEquals(getCached("b"), null);
});

Deno.test("setCached overwrites existing entry", () => {
  clearCache();
  setCached("key", "old", 60000);
  setCached("key", "new", 60000);
  assertEquals(getCached("key"), "new");
});
