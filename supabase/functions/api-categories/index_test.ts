import { assertEquals } from "https://deno.land/std@0.208.0/assert/mod.ts";
import { handler } from "./index.ts";

function setupEnv() {
  Deno.env.set("SUPABASE_URL", "https://test.supabase.co");
  Deno.env.set("SUPABASE_ANON_KEY", "test-anon-key");
}

Deno.test("GET success with 24h cache", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  try {
    setupEnv();
    globalThis.fetch = (_input: string | URL | Request, _init?: RequestInit) => {
      return Promise.resolve(
        new Response('[{"id":"1","name":"Technology","slug":"technology"}]', {
          status: 200,
        }),
      );
    };

    const req = new Request("http://localhost/api-categories");
    const res = await handler(req);

    assertEquals(res.status, 200);
    assertEquals(
      res.headers.get("Cache-Control"),
      "public, max-age=86400",
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
  const req = new Request("http://localhost/api-categories", {
    method: "POST",
  });
  const res = await handler(req);
  assertEquals(res.status, 405);
  const body = await res.json();
  assertEquals(body.error, "Method not allowed");
});

Deno.test("OPTIONS returns CORS 204", async () => {
  const req = new Request("http://localhost/api-categories", {
    method: "OPTIONS",
  });
  const res = await handler(req);
  assertEquals(res.status, 204);
  assertEquals(res.headers.get("Access-Control-Allow-Origin"), "*");
});

Deno.test("error returns 500", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  try {
    setupEnv();
    globalThis.fetch = (_input: string | URL | Request, _init?: RequestInit) => {
      return Promise.reject(new Error("network error"));
    };

    const req = new Request("http://localhost/api-categories");
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
