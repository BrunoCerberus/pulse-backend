import { assert, assertEquals, assertStringIncludes } from "@std/assert";
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

Deno.test("invalid language filter value is dropped", async () => {
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
    const req = new Request("http://localhost/api-articles?language=eq.english");
    await handler(req);
    assertEquals(
      new URL(capturedUrl).searchParams.has("language"),
      false,
      "malformed language must be dropped",
    );
  } finally {
    globalThis.fetch = originalFetch;
    tearDownEnv(originalUrl, originalKey);
  }
});

Deno.test("valid language filter value is forwarded", async () => {
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
    const req = new Request("http://localhost/api-articles?language=eq.en");
    await handler(req);
    assertEquals(
      new URL(capturedUrl).searchParams.get("language"),
      "eq.en",
    );
  } finally {
    globalThis.fetch = originalFetch;
    tearDownEnv(originalUrl, originalKey);
  }
});

Deno.test("non-200 upstream masks PostgREST error body", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  try {
    setupEnv();
    globalThis.fetch = makeMockFetch('{"message":"column "foo" does not exist"}', 400);
    const req = new Request("http://localhost/api-articles");
    const res = await handler(req);
    assertEquals(res.status, 400);
    const body = await res.json();
    assertEquals(body.error, "upstream error");
  } finally {
    globalThis.fetch = originalFetch;
    tearDownEnv(originalUrl, originalKey);
  }
});

Deno.test("list projection omits the heavy content column", async () => {
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
    const req = new Request("http://localhost/api-articles?language=eq.en");
    await handler(req);
    const select = new URL(capturedUrl).searchParams.get("select")!;
    assertEquals(select.split(",").includes("content"), false);
  } finally {
    globalThis.fetch = originalFetch;
    tearDownEnv(originalUrl, originalKey);
  }
});

Deno.test("single-article request selects the detail projection", async () => {
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
    const uuid = "3f2504e0-4f89-11d3-9a0c-0305e82c3301";
    const req = new Request(`http://localhost/api-articles?id=eq.${uuid}&limit=1`);
    await handler(req);
    const params = new URL(capturedUrl).searchParams;
    const select = params.get("select")!.split(",");
    assert(select.includes("content"));
    assert(select.includes("thumbnail_url"));
    assert(select.includes("author"));
    assertEquals(params.get("id"), `eq.${uuid}`);
  } finally {
    globalThis.fetch = originalFetch;
    tearDownEnv(originalUrl, originalKey);
  }
});

Deno.test("malformed id returns empty array instead of an unfiltered list", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  let fetchCalled = false;
  try {
    setupEnv();
    globalThis.fetch = (_input: string | URL | Request, _init?: RequestInit) => {
      fetchCalled = true;
      return Promise.resolve(new Response('[{"id":"1"}]', { status: 200 }));
    };
    const req = new Request("http://localhost/api-articles?id=eq.world/2024/some-slug");
    const res = await handler(req);
    assertEquals(res.status, 200);
    assertEquals(await res.json(), []);
    assertEquals(fetchCalled, false);
  } finally {
    globalThis.fetch = originalFetch;
    tearDownEnv(originalUrl, originalKey);
  }
});

Deno.test("multi-id `in.(...)` gets the list projection, not the detail one", async () => {
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
    const a = "3f2504e0-4f89-11d3-9a0c-0305e82c3301";
    const b = "3f2504e0-4f89-11d3-9a0c-0305e82c3302";
    const req = new Request(`http://localhost/api-articles?id=in.(${a},${b})&limit=100`);
    await handler(req);
    const params = new URL(capturedUrl).searchParams;
    const select = params.get("select")!.split(",");
    // The whole point of the split: content must never ride a multi-row response.
    assertEquals(select.includes("content"), false);
    // Still a legitimate filter — it's forwarded, just without the heavy columns.
    assertEquals(params.get("id"), `in.(${a},${b})`);
  } finally {
    globalThis.fetch = originalFetch;
    tearDownEnv(originalUrl, originalKey);
  }
});

Deno.test("non-eq operators on id get the list projection", async () => {
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
    const uuid = "3f2504e0-4f89-11d3-9a0c-0305e82c3301";
    // `neq.` matches nearly the whole table; `gt.`/`lt.` match broad ranges.
    for (const op of ["neq", "gt", "gte", "lt", "lte"]) {
      const req = new Request(`http://localhost/api-articles?id=${op}.${uuid}`);
      await handler(req);
      const select = new URL(capturedUrl).searchParams.get("select")!.split(",");
      assertEquals(select.includes("content"), false, `${op} leaked content`);
    }
  } finally {
    globalThis.fetch = originalFetch;
    tearDownEnv(originalUrl, originalKey);
  }
});

Deno.test("206 Partial Content is passed through, not masked as an error", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  try {
    setupEnv();
    // PostgREST answers 206 whenever a `limit` makes the response a subset of
    // the matching rows — i.e. every paged list request this endpoint makes.
    globalThis.fetch = makeMockFetch('[{"id":"1","title":"Test"}]', 206, {
      "Content-Range": "0-0/1234",
    });
    const res = await handler(new Request("http://localhost/api-articles?limit=1"));
    assertEquals(res.status, 206);
    assertEquals(res.headers.get("Content-Range"), "0-0/1234");
    const body = await res.json();
    assertEquals(body[0].title, "Test");
    assert(res.headers.get("ETag") !== null);
  } finally {
    globalThis.fetch = originalFetch;
    tearDownEnv(originalUrl, originalKey);
  }
});

Deno.test("genuine upstream errors are still masked", async () => {
  const originalUrl = Deno.env.get("SUPABASE_URL");
  const originalKey = Deno.env.get("SUPABASE_ANON_KEY");
  const originalFetch = globalThis.fetch;
  try {
    setupEnv();
    for (const status of [400, 401, 500, 502]) {
      globalThis.fetch = makeMockFetch('{"message":"column x does not exist"}', status);
      const res = await handler(new Request("http://localhost/api-articles?limit=1"));
      assertEquals(res.status, status);
      assertEquals((await res.json()).error, "upstream error");
    }
  } finally {
    globalThis.fetch = originalFetch;
    tearDownEnv(originalUrl, originalKey);
  }
});
