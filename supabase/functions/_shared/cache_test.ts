import { assertEquals } from "https://deno.land/std@0.208.0/assert/mod.ts";
import { CacheDurations, cacheHeaders } from "./cache.ts";

Deno.test("CacheDurations.CATEGORIES is 24 hours", () => {
  assertEquals(CacheDurations.CATEGORIES, "public, max-age=86400");
});

Deno.test("CacheDurations.SOURCES is 1 hour", () => {
  assertEquals(CacheDurations.SOURCES, "public, max-age=3600");
});

Deno.test("CacheDurations.ARTICLES has stale-while-revalidate", () => {
  assertEquals(
    CacheDurations.ARTICLES,
    "public, max-age=300, stale-while-revalidate=900"
  );
});

Deno.test("CacheDurations.SEARCH is private", () => {
  assertEquals(CacheDurations.SEARCH, "private, max-age=60");
});

Deno.test("cacheHeaders returns object with Cache-Control key", () => {
  const headers = cacheHeaders("public, max-age=3600");
  assertEquals(headers, { "Cache-Control": "public, max-age=3600" });
});

Deno.test("cacheHeaders with custom value", () => {
  const headers = cacheHeaders("no-store");
  assertEquals(headers["Cache-Control"], "no-store");
});

Deno.test("cacheHeaders with CacheDurations constant", () => {
  const headers = cacheHeaders(CacheDurations.ARTICLES);
  assertEquals(
    headers["Cache-Control"],
    "public, max-age=300, stale-while-revalidate=900"
  );
});

Deno.test("cacheHeaders returns only Cache-Control key", () => {
  const headers = cacheHeaders("public, max-age=100");
  const keys = Object.keys(headers);
  assertEquals(keys.length, 1);
  assertEquals(keys[0], "Cache-Control");
});
