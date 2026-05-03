import { assertEquals } from "https://deno.land/std@0.208.0/assert/mod.ts";
import { fetchDatabaseSize, handler } from "./index.ts";
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

// Routes the parallel fetches in handler() to the right canned response so tests
// don't have to track call order. `dbBytes = null` skips the RPC response (404)
// to exercise the "database = null" path.
function mockFetch(rows: unknown, dbBytes: number | null) {
  return (input: string | URL | Request) => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
    if (url.includes("/rpc/get_db_size_bytes")) {
      if (dbBytes === null) {
        return Promise.resolve(new Response("", { status: 404 }));
      }
      return Promise.resolve(new Response(String(dbBytes), { status: 200 }));
    }
    return Promise.resolve(new Response(JSON.stringify(rows), { status: 200 }));
  };
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

Deno.test("GET success returns summary + sources + database with 60s cache", async () => {
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
    // 96.5 MB on a default 500 MB quota → ~19%.
    globalThis.fetch = mockFetch(rows, 96_468_992);

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
    // Database block populated from RPC.
    assertEquals(body.database.size_bytes, 96_468_992);
    assertEquals(body.database.size_pretty, "92.0 MB");
    assertEquals(body.database.quota_pct, 18);
  } finally {
    globalThis.fetch = originalFetch;
    restoreEnv(originalUrl, originalKey);
  }
});

Deno.test("database is null when RPC fails", async () => {
  clearCache();
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  try {
    setupEnv();
    globalThis.fetch = mockFetch([row()], null);

    const req = new Request("http://localhost/api-source-health");
    const res = await handler(req);
    assertEquals(res.status, 200);
    const body = await res.json();
    assertEquals(body.database, null);
    // Source health still works — db failure doesn't cascade.
    assertEquals(body.summary.total, 1);
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
    globalThis.fetch = (input: string | URL | Request) => {
      fetchCount++;
      const url = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      if (url.includes("/rpc/get_db_size_bytes")) {
        return Promise.resolve(new Response("1024", { status: 200 }));
      }
      return Promise.resolve(new Response(JSON.stringify([row()]), { status: 200 }));
    };

    const req = new Request("http://localhost/api-source-health");
    const res1 = await handler(req);
    assertEquals(res1.status, 200);
    // Two upstream calls per uncached request: source_health + RPC.
    assertEquals(fetchCount, 2);

    const res2 = await handler(req);
    assertEquals(res2.status, 200);
    assertEquals(fetchCount, 2, "cache should serve second request");
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
    globalThis.fetch = mockFetch([], 1024);

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

// fetchDatabaseSize unit tests — exercise paths the handler-level tests can't
// trigger directly (env missing, throw, non-numeric body, quota override).

Deno.test("fetchDatabaseSize: returns null when SUPABASE_URL missing", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  try {
    Deno.env.delete("SUPABASE_URL");
    Deno.env.set("SUPABASE_ANON_KEY", "key");
    const result = await fetchDatabaseSize();
    assertEquals(result, null);
  } finally {
    restoreEnv(originalUrl, originalKey);
  }
});

Deno.test("fetchDatabaseSize: returns null when SUPABASE_ANON_KEY missing", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  try {
    Deno.env.set("SUPABASE_URL", "https://test.supabase.co");
    Deno.env.delete("SUPABASE_ANON_KEY");
    const result = await fetchDatabaseSize();
    assertEquals(result, null);
  } finally {
    restoreEnv(originalUrl, originalKey);
  }
});

Deno.test("fetchDatabaseSize: returns null on fetch throw", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  try {
    setupEnv();
    globalThis.fetch = () => Promise.reject(new Error("boom"));
    const result = await fetchDatabaseSize();
    assertEquals(result, null);
  } finally {
    globalThis.fetch = originalFetch;
    restoreEnv(originalUrl, originalKey);
  }
});

Deno.test("fetchDatabaseSize: returns null on non-numeric body", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  try {
    setupEnv();
    globalThis.fetch = () =>
      Promise.resolve(new Response('"not-a-number"', { status: 200 }));
    const result = await fetchDatabaseSize();
    assertEquals(result, null);
  } finally {
    globalThis.fetch = originalFetch;
    restoreEnv(originalUrl, originalKey);
  }
});

Deno.test("fetchDatabaseSize: returns null on negative bytes", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  try {
    setupEnv();
    globalThis.fetch = () =>
      Promise.resolve(new Response("-1", { status: 200 }));
    const result = await fetchDatabaseSize();
    assertEquals(result, null);
  } finally {
    globalThis.fetch = originalFetch;
    restoreEnv(originalUrl, originalKey);
  }
});

Deno.test("fetchDatabaseSize: respects SUPABASE_DB_QUOTA_BYTES override", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalQuota = Deno.env.get("SUPABASE_DB_QUOTA_BYTES");
  const originalFetch = globalThis.fetch;
  try {
    setupEnv();
    Deno.env.set("SUPABASE_DB_QUOTA_BYTES", "200000000"); // 200 MB
    globalThis.fetch = () =>
      Promise.resolve(new Response("100000000", { status: 200 })); // 100 MB
    const result = await fetchDatabaseSize();
    assertEquals(result?.size_bytes, 100_000_000);
    assertEquals(result?.quota_pct, 50);
  } finally {
    globalThis.fetch = originalFetch;
    if (originalQuota) Deno.env.set("SUPABASE_DB_QUOTA_BYTES", originalQuota);
    else Deno.env.delete("SUPABASE_DB_QUOTA_BYTES");
    restoreEnv(originalUrl, originalKey);
  }
});

// Misconfigured SUPABASE_DB_QUOTA_BYTES (empty string, non-numeric, "0",
// negative) must fall back to DEFAULT_QUOTA_BYTES rather than silently emit
// quota_pct: 0 — otherwise the watchdog's threshold check is bypassed and
// no alert ever fires. Default is 524_288_000 (500 MB), so 100 MB → 19%.
Deno.test("fetchDatabaseSize: invalid quota env falls back to default", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalQuota = Deno.env.get("SUPABASE_DB_QUOTA_BYTES");
  const originalFetch = globalThis.fetch;
  try {
    setupEnv();
    globalThis.fetch = () =>
      Promise.resolve(new Response("104857600", { status: 200 })); // 100 MB

    for (const bad of ["", "0", "-1", "500MB", "not-a-number"]) {
      Deno.env.set("SUPABASE_DB_QUOTA_BYTES", bad);
      const result = await fetchDatabaseSize();
      assertEquals(result?.size_bytes, 104_857_600);
      // 100 MB / 500 MB default ≈ 20%.
      assertEquals(result?.quota_pct, 20, `expected fallback for ${JSON.stringify(bad)}`);
    }
  } finally {
    globalThis.fetch = originalFetch;
    if (originalQuota) Deno.env.set("SUPABASE_DB_QUOTA_BYTES", originalQuota);
    else Deno.env.delete("SUPABASE_DB_QUOTA_BYTES");
    restoreEnv(originalUrl, originalKey);
  }
});

Deno.test("fetchDatabaseSize: formats sizes across unit boundaries", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  try {
    setupEnv();
    const cases: [number, string][] = [
      [512, "512 B"],
      [2048, "2.0 KB"],
      [5_242_880, "5.0 MB"],
      [2_147_483_648, "2.00 GB"],
    ];
    for (const [bytes, expected] of cases) {
      globalThis.fetch = () =>
        Promise.resolve(new Response(String(bytes), { status: 200 }));
      const result = await fetchDatabaseSize();
      assertEquals(result?.size_pretty, expected);
    }
  } finally {
    globalThis.fetch = originalFetch;
    restoreEnv(originalUrl, originalKey);
  }
});
