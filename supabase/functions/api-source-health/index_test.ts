import { assertEquals } from "https://deno.land/std@0.208.0/assert/mod.ts";
import { handler } from "./index.ts";
import { clearCache } from "../_shared/memory-cache.ts";

function setupEnv() {
  Deno.env.set("SUPABASE_URL", "https://test.supabase.co");
  Deno.env.set("SUPABASE_ANON_KEY", "test-anon-key");
}

function restoreEnv(originalUrl: string | undefined, originalKey: string | undefined) {
  if (originalUrl) Deno.env.set("SUPABASE_URL", originalUrl);
  else Deno.env.delete("SUPABASE_URL");
  if (originalKey) Deno.env.set("SUPABASE_ANON_KEY", originalKey);
  else Deno.env.delete("SUPABASE_ANON_KEY");
}

// Row shape matches the source_health view from migration 020.
function row(overrides: Record<string, unknown> = {}) {
  return {
    id: crypto.randomUUID(),
    name: "Example",
    slug: "example",
    is_active: true,
    consecutive_failures: 0,
    circuit_open: false,
    circuit_open_until: null,
    last_fetched_at: new Date().toISOString(),
    most_recent_article_at: new Date().toISOString(),
    articles_last_24h: 5,
    ...overrides,
  };
}

Deno.test("GET success returns summary + sources with 60s cache", async () => {
  clearCache();
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  try {
    setupEnv();
    const rows = [
      row({ name: "Healthy" }),
      row({ name: "Tripped", circuit_open: true, consecutive_failures: 7 }),
      row({ name: "Warning", consecutive_failures: 4 }),
      row({
        name: "Stale",
        most_recent_article_at: new Date(Date.now() - 72 * 3600 * 1000).toISOString(),
      }),
      row({ name: "Inactive", is_active: false }),
    ];
    globalThis.fetch = () =>
      Promise.resolve(new Response(JSON.stringify(rows), { status: 200 }));

    const req = new Request("http://localhost/api-source-health");
    const res = await handler(req);
    assertEquals(res.status, 200);
    assertEquals(res.headers.get("Cache-Control"), "public, max-age=60");
    assertEquals(res.headers.get("Content-Type"), "application/json");

    const body = await res.json();
    assertEquals(typeof body.fetched_at, "string");
    assertEquals(body.sources.length, 5);
    assertEquals(body.summary.total, 5);
    assertEquals(body.summary.active, 4);
    assertEquals(body.summary.circuit_open_count, 1);
    // Warning (4 failures, circuit closed) — Tripped (7, circuit open) excluded.
    assertEquals(body.summary.high_failure_count, 1);
    // Stale: active + not-open + no article in 48h.
    assertEquals(body.summary.stale_count, 1);
  } finally {
    globalThis.fetch = originalFetch;
    restoreEnv(originalUrl, originalKey);
  }
});

Deno.test("serves second request from cache", async () => {
  clearCache();
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  try {
    setupEnv();
    let fetchCount = 0;
    globalThis.fetch = () => {
      fetchCount++;
      return Promise.resolve(new Response(JSON.stringify([row()]), { status: 200 }));
    };

    const req = new Request("http://localhost/api-source-health");
    const res1 = await handler(req);
    assertEquals(res1.status, 200);
    assertEquals(fetchCount, 1);

    const res2 = await handler(req);
    assertEquals(res2.status, 200);
    assertEquals(fetchCount, 1, "cache should serve second request");
  } finally {
    globalThis.fetch = originalFetch;
    restoreEnv(originalUrl, originalKey);
  }
});

Deno.test("non-GET returns 405", async () => {
  clearCache();
  const req = new Request("http://localhost/api-source-health", { method: "POST" });
  const res = await handler(req);
  assertEquals(res.status, 405);
  const body = await res.json();
  assertEquals(body.error, "Method not allowed");
});

Deno.test("OPTIONS returns CORS 204", async () => {
  clearCache();
  const req = new Request("http://localhost/api-source-health", { method: "OPTIONS" });
  const res = await handler(req);
  assertEquals(res.status, 204);
  assertEquals(res.headers.get("Access-Control-Allow-Origin"), "*");
});

Deno.test("supabase error returns 500", async () => {
  clearCache();
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  try {
    setupEnv();
    globalThis.fetch = () => Promise.reject(new Error("network error"));

    const req = new Request("http://localhost/api-source-health");
    const res = await handler(req);
    assertEquals(res.status, 500);
  } finally {
    globalThis.fetch = originalFetch;
    restoreEnv(originalUrl, originalKey);
  }
});

Deno.test("empty source list returns zero summary", async () => {
  clearCache();
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  try {
    setupEnv();
    globalThis.fetch = () =>
      Promise.resolve(new Response("[]", { status: 200 }));

    const req = new Request("http://localhost/api-source-health");
    const res = await handler(req);
    assertEquals(res.status, 200);
    const body = await res.json();
    assertEquals(body.summary.total, 0);
    assertEquals(body.summary.active, 0);
    assertEquals(body.summary.stale_count, 0);
  } finally {
    globalThis.fetch = originalFetch;
    restoreEnv(originalUrl, originalKey);
  }
});
