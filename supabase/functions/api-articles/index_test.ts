import {
  assert,
  assertEquals,
  assertStringIncludes,
} from "https://deno.land/std@0.208.0/assert/mod.ts";
import { handler } from "./index.ts";

function setupEnv() {
  Deno.env.set("SUPABASE_URL", "https://test.supabase.co");
  Deno.env.set("SUPABASE_ANON_KEY", "test-anon-key");
}

function tearDownEnv(origUrl?: string, origKey?: string) {
  if (origUrl) Deno.env.set("SUPABASE_URL", origUrl);
  else Deno.env.delete("SUPABASE_URL");
  if (origKey) Deno.env.set("SUPABASE_ANON_KEY", origKey);
  else Deno.env.delete("SUPABASE_ANON_KEY");
}

function makeMockFetch(
  data: string,
  status = 200,
  headers?: Record<string, string>,
) {
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
      "public, max-age=900",
    );
    const etag = res.headers.get("ETag");
    assert(etag !== null && etag !== "");
    assertEquals(res.headers.get("Content-Type"), "application/json");
  } finally {
    globalThis.fetch = originalFetch;
    tearDownEnv(originalUrl, originalKey);
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
  const req = new Request("http://localhost/api-articles", { method: "OPTIONS" });
  const res = await handler(req);
  assertEquals(res.status, 204);
  assertEquals(res.headers.get("Access-Control-Allow-Origin"), "*");
});

Deno.test("ETag 304 Not Modified", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  try {
    setupEnv();
    globalThis.fetch = makeMockFetch('[{"id":"1"}]');
    const req1 = new Request("http://localhost/api-articles");
    const res1 = await handler(req1);
    const etag = res1.headers.get("ETag");
    const req2 = new Request("http://localhost/api-articles", {
      headers: { "If-None-Match": etag! },
    });
    const res2 = await handler(req2);
    assertEquals(res2.status, 304);
  } finally {
    globalThis.fetch = originalFetch;
    tearDownEnv(originalUrl, originalKey);
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
    tearDownEnv(originalUrl, originalKey);
  }
});

Deno.test("fetch error returns 500", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  try {
    setupEnv();
    globalThis.fetch = (
      _input: string | URL | Request,
      _init?: RequestInit,
    ) => Promise.reject(new Error("network error"));
    const req = new Request("http://localhost/api-articles");
    const res = await handler(req);
    assertEquals(res.status, 500);
    const body = await res.json();
    assertEquals(body.error, "Internal server error");
  } finally {
    globalThis.fetch = originalFetch;
    tearDownEnv(originalUrl, originalKey);
  }
});

Deno.test("client cannot override select via query param", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  let capturedUrl = "";
  try {
    setupEnv();
    globalThis.fetch = (input: string | URL | Request, _init?: RequestInit) => {
      capturedUrl = input.toString();
      return Promise.resolve(new Response("[]", { status: 200 }));
    };
    const req = new Request("http://localhost/api-articles?select=*");
    await handler(req);
    // Upstream URL must carry the default projection — never `*`.
    const parsed = new URL(capturedUrl);
    const select = parsed.searchParams.get("select");
    assert(select !== null);
    assert(!select!.includes("*"));
    assertStringIncludes(select!, "id,title,summary");
  } finally {
    globalThis.fetch = originalFetch;
    tearDownEnv(originalUrl, originalKey);
  }
});

Deno.test("limit is clamped to maxLimit (100)", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  let capturedUrl = "";
  try {
    setupEnv();
    globalThis.fetch = (input: string | URL | Request, _init?: RequestInit) => {
      capturedUrl = input.toString();
      return Promise.resolve(new Response("[]", { status: 200 }));
    };
    const req = new Request("http://localhost/api-articles?limit=999999");
    await handler(req);
    assertEquals(new URL(capturedUrl).searchParams.get("limit"), "100");
  } finally {
    globalThis.fetch = originalFetch;
    tearDownEnv(originalUrl, originalKey);
  }
});

Deno.test("oversized request URI returns 414", async () => {
  const big = "x".repeat(5000);
  const req = new Request(`http://localhost/api-articles?slug=${big}`);
  const res = await handler(req);
  assertEquals(res.status, 414);
  const body = await res.json();
  assertEquals(body.error, "Request URI too long");
});

Deno.test("non-200 upstream skips ETag (no 304 replay)", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  try {
    setupEnv();
    globalThis.fetch = makeMockFetch('{"error":"bad"}', 400);
    const req = new Request("http://localhost/api-articles");
    const res = await handler(req);
    assertEquals(res.status, 400);
    assertEquals(res.headers.get("ETag"), null);
  } finally {
    globalThis.fetch = originalFetch;
    tearDownEnv(originalUrl, originalKey);
  }
});

Deno.test("order on non-whitelisted column is dropped", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  let capturedUrl = "";
  try {
    setupEnv();
    globalThis.fetch = (input: string | URL | Request, _init?: RequestInit) => {
      capturedUrl = input.toString();
      return Promise.resolve(new Response("[]", { status: 200 }));
    };
    const req = new Request("http://localhost/api-articles?order=secret.desc");
    await handler(req);
    assertEquals(new URL(capturedUrl).searchParams.has("order"), false);
  } finally {
    globalThis.fetch = originalFetch;
    tearDownEnv(originalUrl, originalKey);
  }
});

Deno.test("order on whitelisted column is forwarded", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  let capturedUrl = "";
  try {
    setupEnv();
    globalThis.fetch = (input: string | URL | Request, _init?: RequestInit) => {
      capturedUrl = input.toString();
      return Promise.resolve(new Response("[]", { status: 200 }));
    };
    const req = new Request(
      "http://localhost/api-articles?order=published_at.desc",
    );
    await handler(req);
    assertEquals(
      new URL(capturedUrl).searchParams.get("order"),
      "published_at.desc",
    );
  } finally {
    globalThis.fetch = originalFetch;
    tearDownEnv(originalUrl, originalKey);
  }
});

Deno.test("empty limit value falls back to defaultLimit", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  let capturedUrl = "";
  try {
    setupEnv();
    globalThis.fetch = (input: string | URL | Request, _init?: RequestInit) => {
      capturedUrl = input.toString();
      return Promise.resolve(new Response("[]", { status: 200 }));
    };
    const req = new Request("http://localhost/api-articles?limit=");
    await handler(req);
    assertEquals(new URL(capturedUrl).searchParams.get("limit"), "100");
  } finally {
    globalThis.fetch = originalFetch;
    tearDownEnv(originalUrl, originalKey);
  }
});
