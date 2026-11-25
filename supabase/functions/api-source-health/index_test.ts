import { assertEquals } from "@std/assert";
import { handler, summarize } from "./index.ts";
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

function urlOf(input: string | URL | Request): string {
  return typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
}

// Answers every upstream call with `rows` and records the URLs requested.
function mockFetch(rows: unknown, calls: string[] = []) {
  return (input: string | URL | Request) => {
    calls.push(urlOf(input));
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

Deno.test("GET success returns summary + sources with 60s cache", async () => {
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
    const calls: string[] = [];
    globalThis.fetch = mockFetch(rows, calls);

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
    assertEquals(typeof body.summary.latest_successful_fetch_at, "string");
    assertEquals(body.summary.fetch_age_minutes, 0);
    // Migration 027 revoked DB size from anon: it tells an attacker filling
    // the database how close the quota is. This endpoint answers anyone, so
    // it must neither return the size nor spend a service-role call on it.
    assertEquals("database" in body, false);
    assertEquals(calls.length, 1);
    assertEquals(calls[0].includes("/rpc/"), false);
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
    globalThis.fetch = () => {
      fetchCount++;
      return Promise.resolve(
        new Response(JSON.stringify([row()]), { status: 200 }),
      );
    };
    const req = new Request("http://localhost/api-source-health");
    await handler(req);
    assertEquals(fetchCount, 1);
    await handler(req);
    assertEquals(fetchCount, 1); // cache hit
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
    globalThis.fetch = () => Promise.resolve(new Response(leak, { status: 400 }));
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
    globalThis.fetch = mockFetch([]);
    const res = await handler(new Request("http://localhost/api-source-health"));
    assertEquals(res.status, 200);
    const body = await res.json();
    assertEquals(body.summary.total, 0);
    assertEquals(body.summary.active, 0);
    assertEquals(body.summary.stale_count, 0);
    assertEquals(body.summary.latest_successful_fetch_at, null);
    assertEquals(body.summary.fetch_age_minutes, null);
  } finally {
    globalThis.fetch = origFetch;
    restoreEnv(origUrl, origKey);
  }
});

// --- summarize ---

Deno.test("summarize counts categories correctly", () => {
  const latest = new Date(Date.now() - 30 * 60 * 1000).toISOString();
  const rows = [
    row({ last_fetched_at: latest }),
    row({ is_active: false }),
    row({ circuit_open: true, consecutive_failures: 10, last_fetched_at: null }),
    row({ consecutive_failures: 5, last_fetched_at: "invalid" }),
  ];
  const s = summarize(rows as unknown as Parameters<typeof summarize>[0]);
  assertEquals(s.total, 4);
  assertEquals(s.active, 3);
  assertEquals(s.circuit_open_count, 1);
  assertEquals(s.high_failure_count, 1);
  assertEquals(s.latest_successful_fetch_at, latest);
  assertEquals(s.fetch_age_minutes, 30);
});

Deno.test("summarize ignores a last_fetched_at beyond the skew tolerance", () => {
  const real = new Date(Date.now() - 200 * 60 * 1000).toISOString();
  const rows = [
    row({ last_fetched_at: real }),
    row({ last_fetched_at: new Date(Date.now() + 365 * 24 * 3600 * 1000).toISOString() }),
  ];
  const s = summarize(rows as unknown as Parameters<typeof summarize>[0]);
  // The future row must not become the fleet maximum — otherwise the age
  // clamps to 0 and the watchdog never fires during an ingestion stall.
  assertEquals(s.latest_successful_fetch_at, real);
  assertEquals(s.fetch_age_minutes, 200);
});

Deno.test("summarize clamps a small forward clock skew to a zero age", () => {
  const rows = [row({ last_fetched_at: new Date(Date.now() + 5 * 60 * 1000).toISOString() })];
  const s = summarize(rows as unknown as Parameters<typeof summarize>[0]);
  assertEquals(s.fetch_age_minutes, 0);
});

Deno.test("partial (206) fleet view is refused, not summarised, and not cached", async () => {
  clearCache();
  const origUrl = Deno.env.get("SUPABASE_URL");
  const origKey = Deno.env.get("SUPABASE_SERVICE_ROLE_KEY");
  const origFetch = globalThis.fetch;
  let healthCalls = 0;
  try {
    setupEnv();
    const rows = [row({ name: "One" }), row({ name: "Two" })];
    globalThis.fetch = () => {
      healthCalls++;
      // PostgREST truncated the rows. Summarising a subset would undercount
      // every field and the watchdog would read it as a healthy fleet.
      return Promise.resolve(
        new Response(JSON.stringify(rows), {
          status: 206,
          headers: { "Content-Range": "0-1/500" },
        }),
      );
    };

    const res = await handler(new Request("http://localhost/api-source-health"));
    assertEquals(res.status, 206);
    assertEquals(res.headers.get("Content-Range"), "0-1/500");
    const body = await res.json();
    assertEquals(body.error, "partial upstream result");
    // No numbers at all beats wrong numbers a consumer would trust.
    assertEquals(body.summary, undefined);
    assertEquals(body.sources, undefined);

    const res2 = await handler(new Request("http://localhost/api-source-health"));
    assertEquals(res2.status, 206);
    assertEquals(healthCalls, 2);
  } finally {
    globalThis.fetch = origFetch;
    restoreEnv(origUrl, origKey);
    clearCache();
  }
});

// The endpoint queries as service_role and caches per canonical query string.
// A filter that matches many rows (`neq.`, `in.(…)`) is a near-full fleet query
// that every random value turns into a new cache key, so it must be dropped:
// the request then collapses onto the cached unfiltered view.
Deno.test("multi-row filters are dropped and served from the unfiltered cache entry", async () => {
  clearCache();
  const origUrl = Deno.env.get("SUPABASE_URL");
  const origKey = Deno.env.get("SUPABASE_SERVICE_ROLE_KEY");
  const origFetch = globalThis.fetch;
  try {
    setupEnv();
    const calls: string[] = [];
    globalThis.fetch = mockFetch([row()], calls);
    const id = crypto.randomUUID();
    for (
      const [param, value] of [
        ["id", `neq.${id}`],
        ["id", `in.(${id},${crypto.randomUUID()})`],
        ["slug", "neq.zzz"],
        ["slug", "in.(bbc-tech,g1)"],
        ["is_active", "neq.false"],
        ["is_active", "in.(true, true)"],
      ]
    ) {
      const qs = `${param}=${encodeURIComponent(value)}`;
      const res = await handler(new Request(`http://localhost/api-source-health?${qs}`));
      assertEquals(res.status, 200, qs);
    }
    assertEquals(calls.length, 1);
    const q = new URL(calls[0]).searchParams;
    assertEquals(q.has("id") || q.has("slug") || q.has("is_active"), false);
  } finally {
    globalThis.fetch = origFetch;
    restoreEnv(origUrl, origKey);
    clearCache();
  }
});

Deno.test("single eq. filters still reach upstream", async () => {
  const origUrl = Deno.env.get("SUPABASE_URL");
  const origKey = Deno.env.get("SUPABASE_SERVICE_ROLE_KEY");
  const origFetch = globalThis.fetch;
  try {
    setupEnv();
    const id = crypto.randomUUID();
    for (
      const [param, value] of [
        ["id", `eq.${id}`],
        ["slug", "eq.bbc-tech"],
        ["is_active", "eq.true"],
      ]
    ) {
      clearCache();
      const calls: string[] = [];
      globalThis.fetch = mockFetch([row()], calls);
      await handler(new Request(`http://localhost/api-source-health?${param}=${value}`));
      assertEquals(new URL(calls[0]).searchParams.get(param), value);
    }
  } finally {
    globalThis.fetch = origFetch;
    restoreEnv(origUrl, origKey);
    clearCache();
  }
});
