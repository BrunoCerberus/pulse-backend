import { assertEquals, assertStringIncludes } from "https://deno.land/std@0.208.0/assert/mod.ts";
import { handler, MAX_QUERY_LEN } from "./index.ts";

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

Deno.test("GET with query returns results", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  try {
    setupEnv();
    globalThis.fetch = (_input: string | URL | Request, _init?: RequestInit) =>
      Promise.resolve(
        new Response('[{"id":"1","title":"Match"}]', { status: 200 }),
      );
    const req = new Request("http://localhost/api-search?q=test&limit=10");
    const res = await handler(req);
    assertEquals(res.status, 200);
    assertStringIncludes(res.headers.get("Cache-Control") ?? "", "max-age=60");
    const body = await res.json();
    assertEquals(body.length, 1);
    assertEquals(body[0].title, "Match");
  } finally {
    globalThis.fetch = originalFetch;
    tearDownEnv(originalUrl, originalKey);
  }
});

Deno.test("empty query returns empty array without hitting upstream", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  let fetchCalled = false;
  try {
    setupEnv();
    globalThis.fetch = (_input: string | URL | Request, _init?: RequestInit) => {
      fetchCalled = true;
      return Promise.resolve(new Response("[]", { status: 200 }));
    };
    const req = new Request("http://localhost/api-search?q=");
    const res = await handler(req);
    assertEquals(res.status, 200);
    assertEquals(await res.json(), []);
    assertEquals(fetchCalled, false);
  } finally {
    globalThis.fetch = originalFetch;
    tearDownEnv(originalUrl, originalKey);
  }
});

Deno.test("oversized query (> MAX_QUERY_LEN) returns empty array without upstream call", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  let fetchCalled = false;
  try {
    setupEnv();
    globalThis.fetch = (_input: string | URL | Request, _init?: RequestInit) => {
      fetchCalled = true;
      return Promise.resolve(new Response("[]", { status: 200 }));
    };
    const huge = "a".repeat(MAX_QUERY_LEN + 1);
    const req = new Request(`http://localhost/api-search?q=${huge}`);
    const res = await handler(req);
    assertEquals(res.status, 200);
    assertEquals(await res.json(), []);
    assertEquals(fetchCalled, false);
  } finally {
    globalThis.fetch = originalFetch;
    tearDownEnv(originalUrl, originalKey);
  }
});

Deno.test("limit capped at 100", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  let capturedBody: string | undefined;
  try {
    setupEnv();
    globalThis.fetch = async (
      input: string | URL | Request,
      init?: RequestInit,
    ) => {
      if (init?.body) capturedBody = init.body as string;
      else if (input instanceof Request) capturedBody = await input.text();
      return new Response("[]", { status: 200 });
    };
    const req = new Request("http://localhost/api-search?q=test&limit=500");
    await handler(req);
    assertEquals(JSON.parse(capturedBody!).result_limit, 100);
  } finally {
    globalThis.fetch = originalFetch;
    tearDownEnv(originalUrl, originalKey);
  }
});

Deno.test("limit clamped to MIN (1)", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  let capturedBody: string | undefined;
  try {
    setupEnv();
    globalThis.fetch = async (
      input: string | URL | Request,
      init?: RequestInit,
    ) => {
      if (init?.body) capturedBody = init.body as string;
      else if (input instanceof Request) capturedBody = await input.text();
      return new Response("[]", { status: 200 });
    };
    const req = new Request("http://localhost/api-search?q=test&limit=-5");
    await handler(req);
    assertEquals(JSON.parse(capturedBody!).result_limit, 1);
  } finally {
    globalThis.fetch = originalFetch;
    tearDownEnv(originalUrl, originalKey);
  }
});

Deno.test("NaN limit falls back to default 20", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  let capturedBody: string | undefined;
  try {
    setupEnv();
    globalThis.fetch = async (
      input: string | URL | Request,
      init?: RequestInit,
    ) => {
      if (init?.body) capturedBody = init.body as string;
      else if (input instanceof Request) capturedBody = await input.text();
      return new Response("[]", { status: 200 });
    };
    const req = new Request("http://localhost/api-search?q=test&limit=abc");
    await handler(req);
    assertEquals(JSON.parse(capturedBody!).result_limit, 20);
  } finally {
    globalThis.fetch = originalFetch;
    tearDownEnv(originalUrl, originalKey);
  }
});

Deno.test("non-GET returns 405", async () => {
  const req = new Request("http://localhost/api-search", { method: "POST" });
  const res = await handler(req);
  assertEquals(res.status, 405);
  const body = await res.json();
  assertEquals(body.error, "Method not allowed");
});

Deno.test("OPTIONS returns CORS 204", async () => {
  const req = new Request("http://localhost/api-search", { method: "OPTIONS" });
  const res = await handler(req);
  assertEquals(res.status, 204);
  assertEquals(res.headers.get("Access-Control-Allow-Origin"), "*");
});

Deno.test("missing env returns 500", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  try {
    Deno.env.delete("SUPABASE_URL");
    Deno.env.delete("SUPABASE_ANON_KEY");
    const req = new Request("http://localhost/api-search?q=test");
    const res = await handler(req);
    assertEquals(res.status, 500);
    const body = await res.json();
    assertEquals(body.error, "Internal server error");
  } finally {
    if (originalUrl) Deno.env.set("SUPABASE_URL", originalUrl);
    if (originalKey) Deno.env.set("SUPABASE_ANON_KEY", originalKey);
  }
});
