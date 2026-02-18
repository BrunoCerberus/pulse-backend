import {
  assertEquals,
  assertRejects,
  assertStringIncludes,
} from "https://deno.land/std@0.208.0/assert/mod.ts";
import { buildProxyUrl, fetchFromSupabase } from "./supabase-proxy.ts";

// --- buildProxyUrl tests ---

Deno.test("buildProxyUrl throws when SUPABASE_URL not set", () => {
  const original = Deno.env.get("SUPABASE_URL");
  try {
    Deno.env.delete("SUPABASE_URL");
    const req = new Request("http://localhost/test");
    const config = { table: "articles", allowedParams: [] };

    try {
      buildProxyUrl(req, config);
      throw new Error("should have thrown");
    } catch (e) {
      assertStringIncludes((e as Error).message, "SUPABASE_URL");
    }
  } finally {
    if (original) Deno.env.set("SUPABASE_URL", original);
  }
});

Deno.test("buildProxyUrl builds correct base URL", () => {
  const original = Deno.env.get("SUPABASE_URL");
  try {
    Deno.env.set("SUPABASE_URL", "https://test.supabase.co");
    const req = new Request("http://localhost/test");
    const config = { table: "articles", allowedParams: [] };

    const url = buildProxyUrl(req, config);
    assertStringIncludes(url, "https://test.supabase.co/rest/v1/articles");
  } finally {
    if (original) Deno.env.set("SUPABASE_URL", original);
    else Deno.env.delete("SUPABASE_URL");
  }
});

Deno.test("buildProxyUrl whitelists allowed params", () => {
  const original = Deno.env.get("SUPABASE_URL");
  try {
    Deno.env.set("SUPABASE_URL", "https://test.supabase.co");
    const req = new Request("http://localhost/test?limit=10&offset=5");
    const config = {
      table: "articles",
      allowedParams: ["limit", "offset"],
    };

    const url = buildProxyUrl(req, config);
    assertStringIncludes(url, "limit=10");
    assertStringIncludes(url, "offset=5");
  } finally {
    if (original) Deno.env.set("SUPABASE_URL", original);
    else Deno.env.delete("SUPABASE_URL");
  }
});

Deno.test("buildProxyUrl drops unknown params", () => {
  const original = Deno.env.get("SUPABASE_URL");
  try {
    Deno.env.set("SUPABASE_URL", "https://test.supabase.co");
    const req = new Request("http://localhost/test?limit=10&evil=drop");
    const config = {
      table: "articles",
      allowedParams: ["limit"],
    };

    const url = buildProxyUrl(req, config);
    assertStringIncludes(url, "limit=10");
    assertEquals(url.includes("evil"), false);
  } finally {
    if (original) Deno.env.set("SUPABASE_URL", original);
    else Deno.env.delete("SUPABASE_URL");
  }
});

Deno.test("buildProxyUrl applies defaultSelect when select not provided", () => {
  const original = Deno.env.get("SUPABASE_URL");
  try {
    Deno.env.set("SUPABASE_URL", "https://test.supabase.co");
    const req = new Request("http://localhost/test");
    const config = {
      table: "articles",
      allowedParams: ["select"],
      defaultSelect: "id,title",
    };

    const url = buildProxyUrl(req, config);
    assertStringIncludes(url, "select=id%2Ctitle");
  } finally {
    if (original) Deno.env.set("SUPABASE_URL", original);
    else Deno.env.delete("SUPABASE_URL");
  }
});

Deno.test("buildProxyUrl uses explicit select over defaultSelect", () => {
  const original = Deno.env.get("SUPABASE_URL");
  try {
    Deno.env.set("SUPABASE_URL", "https://test.supabase.co");
    const req = new Request("http://localhost/test?select=id");
    const config = {
      table: "articles",
      allowedParams: ["select"],
      defaultSelect: "id,title,summary",
    };

    const url = buildProxyUrl(req, config);
    // Should have the explicit select=id, not the default
    const parsed = new URL(url);
    assertEquals(parsed.searchParams.get("select"), "id");
  } finally {
    if (original) Deno.env.set("SUPABASE_URL", original);
    else Deno.env.delete("SUPABASE_URL");
  }
});

Deno.test("buildProxyUrl handles request with no query params", () => {
  const original = Deno.env.get("SUPABASE_URL");
  try {
    Deno.env.set("SUPABASE_URL", "https://test.supabase.co");
    const req = new Request("http://localhost/test");
    const config = { table: "categories", allowedParams: ["select", "order"] };

    const url = buildProxyUrl(req, config);
    assertStringIncludes(url, "https://test.supabase.co/rest/v1/categories");
  } finally {
    if (original) Deno.env.set("SUPABASE_URL", original);
    else Deno.env.delete("SUPABASE_URL");
  }
});

// --- fetchFromSupabase tests ---

Deno.test("fetchFromSupabase throws when SUPABASE_ANON_KEY not set", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  try {
    Deno.env.set("SUPABASE_URL", "https://test.supabase.co");
    Deno.env.delete("SUPABASE_ANON_KEY");
    const req = new Request("http://localhost/test");
    const config = { table: "articles", allowedParams: [] };

    await assertRejects(
      () => fetchFromSupabase(req, config),
      Error,
      "SUPABASE_ANON_KEY",
    );
  } finally {
    if (originalUrl) Deno.env.set("SUPABASE_URL", originalUrl);
    else Deno.env.delete("SUPABASE_URL");
    if (originalKey) Deno.env.set("SUPABASE_ANON_KEY", originalKey);
  }
});

Deno.test("fetchFromSupabase returns data on success", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  try {
    Deno.env.set("SUPABASE_URL", "https://test.supabase.co");
    Deno.env.set("SUPABASE_ANON_KEY", "test-key");

    globalThis.fetch = (_input: string | URL | Request, _init?: RequestInit) => {
      return Promise.resolve(
        new Response('[{"id":"1"}]', {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      );
    };

    const req = new Request("http://localhost/test");
    const config = { table: "articles", allowedParams: [] };
    const result = await fetchFromSupabase(req, config);

    assertEquals(result.status, 200);
    assertEquals(result.data, '[{"id":"1"}]');
  } finally {
    globalThis.fetch = originalFetch;
    if (originalUrl) Deno.env.set("SUPABASE_URL", originalUrl);
    else Deno.env.delete("SUPABASE_URL");
    if (originalKey) Deno.env.set("SUPABASE_ANON_KEY", originalKey);
    else Deno.env.delete("SUPABASE_ANON_KEY");
  }
});

Deno.test("fetchFromSupabase forwards content-range header", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  try {
    Deno.env.set("SUPABASE_URL", "https://test.supabase.co");
    Deno.env.set("SUPABASE_ANON_KEY", "test-key");

    globalThis.fetch = (_input: string | URL | Request, _init?: RequestInit) => {
      return Promise.resolve(
        new Response("[]", {
          status: 200,
          headers: {
            "Content-Type": "application/json",
            "Content-Range": "0-9/100",
          },
        }),
      );
    };

    const req = new Request("http://localhost/test");
    const config = { table: "articles", allowedParams: [] };
    const result = await fetchFromSupabase(req, config);

    assertEquals(result.contentRange, "0-9/100");
  } finally {
    globalThis.fetch = originalFetch;
    if (originalUrl) Deno.env.set("SUPABASE_URL", originalUrl);
    else Deno.env.delete("SUPABASE_URL");
    if (originalKey) Deno.env.set("SUPABASE_ANON_KEY", originalKey);
    else Deno.env.delete("SUPABASE_ANON_KEY");
  }
});

Deno.test("fetchFromSupabase proxies non-200 status", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  try {
    Deno.env.set("SUPABASE_URL", "https://test.supabase.co");
    Deno.env.set("SUPABASE_ANON_KEY", "test-key");

    globalThis.fetch = (_input: string | URL | Request, _init?: RequestInit) => {
      return Promise.resolve(
        new Response('{"error":"bad request"}', { status: 400 }),
      );
    };

    const req = new Request("http://localhost/test");
    const config = { table: "articles", allowedParams: [] };
    const result = await fetchFromSupabase(req, config);

    assertEquals(result.status, 400);
  } finally {
    globalThis.fetch = originalFetch;
    if (originalUrl) Deno.env.set("SUPABASE_URL", originalUrl);
    else Deno.env.delete("SUPABASE_URL");
    if (originalKey) Deno.env.set("SUPABASE_ANON_KEY", originalKey);
    else Deno.env.delete("SUPABASE_ANON_KEY");
  }
});

Deno.test("fetchFromSupabase sends Prefer count=estimated header", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  let capturedHeaders: Headers | undefined;
  try {
    Deno.env.set("SUPABASE_URL", "https://test.supabase.co");
    Deno.env.set("SUPABASE_ANON_KEY", "test-key");

    globalThis.fetch = (input: string | URL | Request, init?: RequestInit) => {
      if (init?.headers) {
        capturedHeaders = new Headers(init.headers as HeadersInit);
      } else if (input instanceof Request) {
        capturedHeaders = input.headers;
      }
      return Promise.resolve(new Response("[]", { status: 200 }));
    };

    const req = new Request("http://localhost/test");
    const config = { table: "articles", allowedParams: [] };
    await fetchFromSupabase(req, config);

    assertEquals(capturedHeaders?.get("Prefer"), "count=estimated");
  } finally {
    globalThis.fetch = originalFetch;
    if (originalUrl) Deno.env.set("SUPABASE_URL", originalUrl);
    else Deno.env.delete("SUPABASE_URL");
    if (originalKey) Deno.env.set("SUPABASE_ANON_KEY", originalKey);
    else Deno.env.delete("SUPABASE_ANON_KEY");
  }
});
