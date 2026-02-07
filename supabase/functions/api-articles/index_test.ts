import {
  assertEquals,
  assertStringIncludes,
} from "https://deno.land/std@0.208.0/assert/mod.ts";
import { handler } from "./index.ts";

function setupEnv() {
  Deno.env.set("SUPABASE_URL", "https://test.supabase.co");
  Deno.env.set("SUPABASE_ANON_KEY", "test-anon-key");
}

function makeMockFetch(data: string, status = 200, headers?: Record<string, string>) {
  return (_input: string | URL | Request, _init?: RequestInit) => {
    return Promise.resolve(
      new Response(data, { status, headers: headers ?? {} }),
    );
  };
}

Deno.test("GET success returns articles with cache and ETag", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  try {
    setupEnv();
    globalThis.fetch = makeMockFetch('[{"id":"1","title":"Test"}]');

    const req = new Request("http://localhost/api-articles?limit=5");
    const res = await handler(req);

    assertEquals(res.status, 200);
    assertStringIncludes(
      res.headers.get("Cache-Control") ?? "",
      "public, max-age=300",
    );
    const etag = res.headers.get("ETag");
    assertEquals(etag !== null && etag !== "", true);
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
  const req = new Request("http://localhost/api-articles", { method: "POST" });
  const res = await handler(req);
  assertEquals(res.status, 405);
  const body = await res.json();
  assertEquals(body.error, "Method not allowed");
});

Deno.test("OPTIONS returns CORS 204", async () => {
  const req = new Request("http://localhost/api-articles", {
    method: "OPTIONS",
  });
  const res = await handler(req);
  assertEquals(res.status, 204);
  assertEquals(
    res.headers.get("Access-Control-Allow-Origin"),
    "*",
  );
});

Deno.test("ETag 304 Not Modified", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  try {
    setupEnv();
    globalThis.fetch = makeMockFetch('[{"id":"1"}]');

    // First request to get the ETag
    const req1 = new Request("http://localhost/api-articles");
    const res1 = await handler(req1);
    const etag = res1.headers.get("ETag");

    // Second request with If-None-Match
    const req2 = new Request("http://localhost/api-articles", {
      headers: { "If-None-Match": etag! },
    });
    const res2 = await handler(req2);
    assertEquals(res2.status, 304);
  } finally {
    globalThis.fetch = originalFetch;
    if (originalUrl) Deno.env.set("SUPABASE_URL", originalUrl);
    else Deno.env.delete("SUPABASE_URL");
    if (originalKey) Deno.env.set("SUPABASE_ANON_KEY", originalKey);
    else Deno.env.delete("SUPABASE_ANON_KEY");
  }
});

Deno.test("Content-Range header forwarded", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  try {
    setupEnv();
    globalThis.fetch = makeMockFetch("[]", 200, { "Content-Range": "0-9/50" });

    const req = new Request("http://localhost/api-articles");
    const res = await handler(req);

    assertEquals(res.headers.get("Content-Range"), "0-9/50");
  } finally {
    globalThis.fetch = originalFetch;
    if (originalUrl) Deno.env.set("SUPABASE_URL", originalUrl);
    else Deno.env.delete("SUPABASE_URL");
    if (originalKey) Deno.env.set("SUPABASE_ANON_KEY", originalKey);
    else Deno.env.delete("SUPABASE_ANON_KEY");
  }
});

Deno.test("fetch error returns 500", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  try {
    setupEnv();
    globalThis.fetch = (_input: string | URL | Request, _init?: RequestInit) => {
      return Promise.reject(new Error("network error"));
    };

    const req = new Request("http://localhost/api-articles");
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
