import { assertEquals } from "https://deno.land/std@0.208.0/assert/mod.ts";
import { fetchDatabaseSize, handler, summarize } from "./index.ts";
import { clearCache } from "../_shared/memory-cache.ts";

function setupEnv() {
  Deno.env.set("SUPABASE_URL", "https://test.supabase.co");
  Deno.env.set("SUPABASE_SERVICE_ROLE_KEY", "test-service-key");
}

function restoreEnv(originalUrl?: string, originalKey?: string) {
  if (originalUrl) Deno.env.set("SUPABASE_URL", originalUrl);
  else Deno.env.delete("SUPABASE_URL");
  if (originalKey) Deno.env.set("SUPABASE_SERVICE_ROLE_KEY", originalKey);
  else Deno.env.delete("SUPABASE_SERVICE_ROLE_KEY");
}

// Routes parallel handler fetches by URL pattern.
function mockFetch(rows: unknown, dbBytes: number | null) {
  return (input: string | URL | Request) => {
    const url = typeof input === "string"
      ? input
      : input instanceof URL
      ? input.toString()
      : input.url;
    if (url.includes("/rpc/get_db_size_bytes")) {
      if (dbBytes === null) return Promise.resolve(new Response("", { status: 404 }));
      return Promise.resolve(new Response(String(dbBytes), { status: 200 }));
    }
    return Promise.resolve(new Response(JSON.stringify(rows), { status: 200 }));
  };
}

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
  const origUrl = Deno.env.get("SUPABASE_URL");
  const origKey = Deno.env.get("SUPABASE_SERVICE_ROLE_KEY");
  const origFetch = globalThis.fetch;
  try {
    setupEnv();
    const rows = [
      row({ name: "Healthy" }),
      row({ name: "Tripped", circuit_open: true, consecutive_failures: 7 }),
      row({ name: "Warning", consecutive_failures: 4 }),
      row({
        name: "Stale",
        most_recent_article_at: new Date(
          Date.now() - 8 * 24 * 3600 * 1000,
        ).toISOString(),
      }),
      row({ name: "Inactive", is_active: false }),
    ];
    globalThis.fetch = mockFetch(rows, 96_468_992);

    const req = new Request("http://localhost/api-source-health");
    const res = await handler(req);
    assertEquals(res.status, 200);
    assertEquals(res.headers.get("Cache-Control"), "public, max-age=60");

    const body = await res.json();
    assertEquals(body.sources.length, 5);
    assertEquals(body.summary.total, 5);
    assertEquals(body.summary.active, 4);
    assertEquals(body.summary.circuit_open_count, 1);
    assertEquals(body.summary.high_failure_count, 1);
    assertEquals(body.summary.stale_count, 1);
    assertEquals(body.database.size_bytes, 96_468_992);
    assertEquals(body.database.size_pretty, "92.0 MB");
    assertEquals(body.database.quota_pct, 18);
  } finally {
    globalThis.fetch = origFetch;
    restoreEnv(origUrl, origKey);
  }
});

Deno.test("database is null when RPC fails", async () => {
  clearCache();
  const origUrl = Deno.env.get("SUPABASE_URL");
  const origKey = Deno.env.get("SUPABASE_SERVICE_ROLE_KEY");
  const origFetch = globalThis.fetch;
  try {
    setupEnv();
    globalThis.fetch = mockFetch([row()], null);
    const res = await handler(new Request("http://localhost/api-source-health"));
    assertEquals(res.status, 200);
    const body = await res.json();
    assertEquals(body.database, null);
    assertEquals(body.summary.total, 1);
  } finally {
    globalThis.fetch = origFetch;
    restoreEnv(origUrl, origKey);
  }
});

Deno.test("serves second request from cache", async () => {
  clearCache();
  const origUrl = Deno.env.get("SUPABASE_URL");
  const origKey = Deno.env.get("SUPABASE_SERVICE_ROLE_KEY");
  const origFetch = globalThis.fetch;
  try {
    setupEnv();
    let fetchCount = 0;
    globalThis.fetch = (input: string | URL | Request) => {
      fetchCount++;
      const url = typeof input === "string"
        ? input
        : input instanceof URL
        ? input.toString()
        : input.url;
      if (url.includes("/rpc/get_db_size_bytes")) {
        return Promise.resolve(new Response("1024", { status: 200 }));
      }
      return Promise.resolve(
        new Response(JSON.stringify([row()]), { status: 200 }),
      );
    };
    const req = new Request("http://localhost/api-source-health");
    await handler(req);
    assertEquals(fetchCount, 2); // source_health + RPC
    await handler(req);
    assertEquals(fetchCount, 2); // cache hit
  } finally {
    globalThis.fetch = origFetch;
    restoreEnv(origUrl, origKey);
  }
});

Deno.test("authenticates upstream with service-role key", async () => {
  clearCache();
  const origUrl = Deno.env.get("SUPABASE_URL");
  const origKey = Deno.env.get("SUPABASE_SERVICE_ROLE_KEY");
  const origFetch = globalThis.fetch;
  let capturedHeaders: Headers | undefined;
  try {
    setupEnv();
    globalThis.fetch = (input: string | URL | Request, init?: RequestInit) => {
      if (init?.headers) capturedHeaders = new Headers(init.headers as HeadersInit);
      else if (input instanceof Request) capturedHeaders = input.headers;
      const url = typeof input === "string"
        ? input
        : input instanceof URL
        ? input.toString()
        : input.url;
      if (url.includes("/rpc/get_db_size_bytes")) {
        return Promise.resolve(new Response("1024", { status: 200 }));
      }
      return Promise.resolve(new Response("[]", { status: 200 }));
    };
    await handler(new Request("http://localhost/api-source-health"));
    assertEquals(capturedHeaders?.get("apikey"), "test-service-key");
    assertEquals(
      capturedHeaders?.get("Authorization"),
      "Bearer test-service-key",
    );
  } finally {
    globalThis.fetch = origFetch;
    restoreEnv(origUrl, origKey);
  }
});

Deno.test("returns 500 when service-role key missing", async () => {
  clearCache();
  const origUrl = Deno.env.get("SUPABASE_URL");
  const origKey = Deno.env.get("SUPABASE_SERVICE_ROLE_KEY");
  try {
    Deno.env.set("SUPABASE_URL", "https://test.supabase.co");
    Deno.env.delete("SUPABASE_SERVICE_ROLE_KEY");
    const res = await handler(new Request("http://localhost/api-source-health"));
    assertEquals(res.status, 500);
    const body = await res.json();
    assertEquals(body.error, "Service configuration missing");
  } finally {
    restoreEnv(origUrl, origKey);
  }
});

Deno.test("non-GET returns 405", async () => {
  clearCache();
  const res = await handler(
    new Request("http://localhost/api-source-health", { method: "POST" }),
  );
  assertEquals(res.status, 405);
});

Deno.test("OPTIONS returns CORS 204", async () => {
  clearCache();
  const res = await handler(
    new Request("http://localhost/api-source-health", { method: "OPTIONS" }),
  );
  assertEquals(res.status, 204);
  assertEquals(res.headers.get("Access-Control-Allow-Origin"), "*");
});

Deno.test("oversized request URI returns 414", async () => {
  clearCache();
  const big = "x".repeat(5000);
  const req = new Request(`http://localhost/api-source-health?slug=${big}`);
  const res = await handler(req);
  assertEquals(res.status, 414);
});

Deno.test("upstream failure returns 500", async () => {
  clearCache();
  const origUrl = Deno.env.get("SUPABASE_URL");
  const origKey = Deno.env.get("SUPABASE_SERVICE_ROLE_KEY");
  const origFetch = globalThis.fetch;
  try {
    setupEnv();
    globalThis.fetch = () => Promise.reject(new Error("network error"));
    const res = await handler(new Request("http://localhost/api-source-health"));
    assertEquals(res.status, 500);
  } finally {
    globalThis.fetch = origFetch;
    restoreEnv(origUrl, origKey);
  }
});

Deno.test("non-200 upstream does not echo the service-role error body", async () => {
  clearCache();
  const origUrl = Deno.env.get("SUPABASE_URL");
  const origKey = Deno.env.get("SUPABASE_SERVICE_ROLE_KEY");
  const origFetch = globalThis.fetch;
  try {
    setupEnv();
    // source_health is queried as service_role; a malformed client param makes
    // PostgREST return a 400 with a revealing SQL error body. That body must NOT
    // reach the anonymous caller — it would disclose internals anon can't read.
    const leak = JSON.stringify({
      code: "22P02",
      message: "invalid input syntax for type uuid: notauuid",
    });
    globalThis.fetch = (input: string | URL | Request) => {
      const url = typeof input === "string"
        ? input
        : input instanceof URL
        ? input.toString()
        : input.url;
      if (url.includes("/rpc/get_db_size_bytes")) {
        return Promise.resolve(new Response("0", { status: 200 }));
      }
      return Promise.resolve(new Response(leak, { status: 400 }));
    };
    const res = await handler(
      new Request("http://localhost/api-source-health?id=notauuid"),
    );
    assertEquals(res.status, 400); // upstream status preserved
    const text = await res.text();
    assertEquals(text.includes("22P02"), false);
    assertEquals(text.includes("invalid input syntax"), false);
    assertEquals(JSON.parse(text).error, "upstream error");
  } finally {
    globalThis.fetch = origFetch;
    restoreEnv(origUrl, origKey);
  }
});

Deno.test("empty source list yields zero summary", async () => {
  clearCache();
  const origUrl = Deno.env.get("SUPABASE_URL");
  const origKey = Deno.env.get("SUPABASE_SERVICE_ROLE_KEY");
  const origFetch = globalThis.fetch;
  try {
    setupEnv();
    globalThis.fetch = mockFetch([], 1024);
    const res = await handler(new Request("http://localhost/api-source-health"));
    assertEquals(res.status, 200);
    const body = await res.json();
    assertEquals(body.summary.total, 0);
    assertEquals(body.summary.active, 0);
    assertEquals(body.summary.stale_count, 0);
  } finally {
    globalThis.fetch = origFetch;
    restoreEnv(origUrl, origKey);
  }
});

// --- fetchDatabaseSize ---

Deno.test("fetchDatabaseSize: null when SUPABASE_URL missing", async () => {
  const origUrl = Deno.env.get("SUPABASE_URL");
  const origKey = Deno.env.get("SUPABASE_SERVICE_ROLE_KEY");
  try {
    Deno.env.delete("SUPABASE_URL");
    Deno.env.set("SUPABASE_SERVICE_ROLE_KEY", "k");
    assertEquals(await fetchDatabaseSize(), null);
  } finally {
    restoreEnv(origUrl, origKey);
  }
});

Deno.test("fetchDatabaseSize: null when service-role key missing", async () => {
  const origUrl = Deno.env.get("SUPABASE_URL");
  const origKey = Deno.env.get("SUPABASE_SERVICE_ROLE_KEY");
  try {
    Deno.env.set("SUPABASE_URL", "https://test.supabase.co");
    Deno.env.delete("SUPABASE_SERVICE_ROLE_KEY");
    assertEquals(await fetchDatabaseSize(), null);
  } finally {
    restoreEnv(origUrl, origKey);
  }
});

Deno.test("fetchDatabaseSize: null on fetch throw", async () => {
  const origUrl = Deno.env.get("SUPABASE_URL");
  const origKey = Deno.env.get("SUPABASE_SERVICE_ROLE_KEY");
  const origFetch = globalThis.fetch;
  try {
    setupEnv();
    globalThis.fetch = () => Promise.reject(new Error("boom"));
    assertEquals(await fetchDatabaseSize(), null);
  } finally {
    globalThis.fetch = origFetch;
    restoreEnv(origUrl, origKey);
  }
});

Deno.test("fetchDatabaseSize: null on non-numeric body", async () => {
  const origUrl = Deno.env.get("SUPABASE_URL");
  const origKey = Deno.env.get("SUPABASE_SERVICE_ROLE_KEY");
  const origFetch = globalThis.fetch;
  try {
    setupEnv();
    globalThis.fetch = () => Promise.resolve(new Response('"not-a-number"', { status: 200 }));
    assertEquals(await fetchDatabaseSize(), null);
  } finally {
    globalThis.fetch = origFetch;
    restoreEnv(origUrl, origKey);
  }
});

Deno.test("fetchDatabaseSize: null on negative bytes", async () => {
  const origUrl = Deno.env.get("SUPABASE_URL");
  const origKey = Deno.env.get("SUPABASE_SERVICE_ROLE_KEY");
  const origFetch = globalThis.fetch;
  try {
    setupEnv();
    globalThis.fetch = () => Promise.resolve(new Response("-1", { status: 200 }));
    assertEquals(await fetchDatabaseSize(), null);
  } finally {
    globalThis.fetch = origFetch;
    restoreEnv(origUrl, origKey);
  }
});

Deno.test("fetchDatabaseSize: respects SUPABASE_DB_QUOTA_BYTES override", async () => {
  const origUrl = Deno.env.get("SUPABASE_URL");
  const origKey = Deno.env.get("SUPABASE_SERVICE_ROLE_KEY");
  const origQuota = Deno.env.get("SUPABASE_DB_QUOTA_BYTES");
  const origFetch = globalThis.fetch;
  try {
    setupEnv();
    Deno.env.set("SUPABASE_DB_QUOTA_BYTES", "200000000");
    globalThis.fetch = () => Promise.resolve(new Response("100000000", { status: 200 }));
    const result = await fetchDatabaseSize();
    assertEquals(result?.size_bytes, 100_000_000);
    assertEquals(result?.quota_pct, 50);
  } finally {
    globalThis.fetch = origFetch;
    if (origQuota) Deno.env.set("SUPABASE_DB_QUOTA_BYTES", origQuota);
    else Deno.env.delete("SUPABASE_DB_QUOTA_BYTES");
    restoreEnv(origUrl, origKey);
  }
});

Deno.test("fetchDatabaseSize: invalid quota env falls back to default", async () => {
  const origUrl = Deno.env.get("SUPABASE_URL");
  const origKey = Deno.env.get("SUPABASE_SERVICE_ROLE_KEY");
  const origQuota = Deno.env.get("SUPABASE_DB_QUOTA_BYTES");
  const origFetch = globalThis.fetch;
  try {
    setupEnv();
    globalThis.fetch = () => Promise.resolve(new Response("104857600", { status: 200 }));
    for (const bad of ["", "0", "-1", "500MB", "not-a-number"]) {
      Deno.env.set("SUPABASE_DB_QUOTA_BYTES", bad);
      const result = await fetchDatabaseSize();
      assertEquals(result?.size_bytes, 104_857_600);
      assertEquals(result?.quota_pct, 20);
    }
  } finally {
    globalThis.fetch = origFetch;
    if (origQuota) Deno.env.set("SUPABASE_DB_QUOTA_BYTES", origQuota);
    else Deno.env.delete("SUPABASE_DB_QUOTA_BYTES");
    restoreEnv(origUrl, origKey);
  }
});

Deno.test("fetchDatabaseSize: formats sizes across unit boundaries", async () => {
  const origUrl = Deno.env.get("SUPABASE_URL");
  const origKey = Deno.env.get("SUPABASE_SERVICE_ROLE_KEY");
  const origFetch = globalThis.fetch;
  try {
    setupEnv();
    const cases: [number, string][] = [
      [512, "512 B"],
      [2048, "2.0 KB"],
      [5_242_880, "5.0 MB"],
      [2_147_483_648, "2.00 GB"],
    ];
    for (const [bytes, expected] of cases) {
      globalThis.fetch = () => Promise.resolve(new Response(String(bytes), { status: 200 }));
      const result = await fetchDatabaseSize();
      assertEquals(result?.size_pretty, expected);
    }
  } finally {
    globalThis.fetch = origFetch;
    restoreEnv(origUrl, origKey);
  }
});

// --- summarize ---

Deno.test("summarize counts categories correctly", () => {
  const rows = [
    row(),
    row({ is_active: false }),
    row({ circuit_open: true, consecutive_failures: 10 }),
    row({ consecutive_failures: 5 }),
  ];
  const s = summarize(rows as unknown as Parameters<typeof summarize>[0]);
  assertEquals(s.total, 4);
  assertEquals(s.active, 3);
  assertEquals(s.circuit_open_count, 1);
  assertEquals(s.high_failure_count, 1);
});
