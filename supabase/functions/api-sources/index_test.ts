import { assertEquals } from "https://deno.land/std@0.208.0/assert/mod.ts";
import { handler } from "./index.ts";
import { clearCache } from "../_shared/memory-cache.ts";

function setupEnv() {
  Deno.env.set("SUPABASE_URL", "https://test.supabase.co");
  Deno.env.set("SUPABASE_ANON_KEY", "test-anon-key");
}

Deno.test("GET success with 1h cache", async () => {
  clearCache();
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  try {
    setupEnv();
    globalThis.fetch = (_input: string | URL | Request, _init?: RequestInit) => {
      return Promise.resolve(
        new Response('[{"id":"1","name":"BBC News","slug":"bbc-news"}]', {
          status: 200,
        }),
      );
    };

    const req = new Request("http://localhost/api-sources");
    const res = await handler(req);

    assertEquals(res.status, 200);
    assertEquals(
      res.headers.get("Cache-Control"),
      "public, max-age=3600",
    );
    assertEquals(res.headers.get("Content-Type"), "application/json");
  } finally {
    globalThis.fetch = originalFetch;
    if (originalUrl) Deno.env.set("SUPABASE_URL", originalUrl);
    else Deno.env.delete("SUPABASE_URL");
    if (originalKey) Deno.env.set("SUPABASE_ANON_KEY", originalKey);
    else Deno.env.delete("SUPABASE_ANON_KEY");
  }
});

Deno.test("non-GET returns 405", async () => {
  clearCache();
  const req = new Request("http://localhost/api-sources", { method: "PUT" });
  const res = await handler(req);
  assertEquals(res.status, 405);
  const body = await res.json();
  assertEquals(body.error, "Method not allowed");
});

Deno.test("OPTIONS returns CORS 204", async () => {
  clearCache();
  const req = new Request("http://localhost/api-sources", {
    method: "OPTIONS",
  });
  const res = await handler(req);
  assertEquals(res.status, 204);
  assertEquals(res.headers.get("Access-Control-Allow-Origin"), "*");
});

Deno.test("error returns 500", async () => {
  clearCache();
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  try {
    setupEnv();
    globalThis.fetch = (_input: string | URL | Request, _init?: RequestInit) => {
      return Promise.reject(new Error("network error"));
    };

    const req = new Request("http://localhost/api-sources");
    const res = await handler(req);

    assertEquals(res.status, 500);
    const body = await res.json();
    assertEquals(body.error, "Internal server error");
  } finally {
    globalThis.fetch = originalFetch;
    if (originalUrl) Deno.env.set("SUPABASE_URL", originalUrl);
    else Deno.env.delete("SUPABASE_URL");
    if (originalKey) Deno.env.set("SUPABASE_ANON_KEY", originalKey);
    else Deno.env.delete("SUPABASE_ANON_KEY");
  }
});

Deno.test("serves from cache on second request", async () => {
  clearCache();
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  try {
    setupEnv();
    let fetchCount = 0;
    globalThis.fetch = (_input: string | URL | Request, _init?: RequestInit) => {
      fetchCount++;
      return Promise.resolve(
        new Response('[{"id":"1","name":"BBC News","slug":"bbc-news"}]', {
          status: 200,
        }),
      );
    };

    const req1 = new Request("http://localhost/api-sources");
    const res1 = await handler(req1);
    assertEquals(res1.status, 200);
    assertEquals(fetchCount, 1);

    const req2 = new Request("http://localhost/api-sources");
    const res2 = await handler(req2);
    assertEquals(res2.status, 200);
    assertEquals(fetchCount, 1, "expected no additional fetch for cached response");
  } finally {
    globalThis.fetch = originalFetch;
    if (originalUrl) Deno.env.set("SUPABASE_URL", originalUrl);
    else Deno.env.delete("SUPABASE_URL");
    if (originalKey) Deno.env.set("SUPABASE_ANON_KEY", originalKey);
    else Deno.env.delete("SUPABASE_ANON_KEY");
  }
});
