import {
  assert,
  assertEquals,
  assertRejects,
  assertStringIncludes,
} from "https://deno.land/std@0.208.0/assert/mod.ts";
import {
  buildCacheKey,
  buildProxyUrl,
  fetchFromSupabase,
  isBooleanFilter,
  isCacheableResult,
  isLanguageFilter,
  isSlugFilter,
  isUuidFilter,
  MAX_QUERY_STRING_LEN,
  type ProxyConfig,
  tooLong,
} from "./supabase-proxy.ts";

const baseConfig: ProxyConfig = {
  table: "articles",
  allowedParams: [],
  defaultSelect: "id,title",
};

function withEnv(
  env: Record<string, string | undefined>,
  fn: () => void | Promise<void>,
): () => Promise<void> {
  return async () => {
    const originals: Record<string, string | undefined> = {};
    for (const k of Object.keys(env)) {
      originals[k] = Deno.env.get(k);
    }
    try {
      for (const [k, v] of Object.entries(env)) {
        if (v === undefined) Deno.env.delete(k);
        else Deno.env.set(k, v);
      }
      await fn();
    } finally {
      for (const [k, v] of Object.entries(originals)) {
        if (v === undefined) Deno.env.delete(k);
        else Deno.env.set(k, v);
      }
    }
  };
}

// --- buildProxyUrl ---

Deno.test(
  "buildProxyUrl throws when SUPABASE_URL not set",
  withEnv({ SUPABASE_URL: undefined }, () => {
    const req = new Request("http://localhost/test");
    try {
      buildProxyUrl(req, baseConfig);
      throw new Error("should have thrown");
    } catch (e) {
      assertStringIncludes((e as Error).message, "SUPABASE_URL");
    }
  }),
);

Deno.test(
  "buildProxyUrl builds the upstream URL with defaultSelect always applied",
  withEnv({ SUPABASE_URL: "https://test.supabase.co" }, () => {
    const req = new Request("http://localhost/test");
    const url = buildProxyUrl(req, baseConfig);
    assertStringIncludes(url, "https://test.supabase.co/rest/v1/articles");
    const parsed = new URL(url);
    assertEquals(parsed.searchParams.get("select"), "id,title");
  }),
);

Deno.test(
  "buildProxyUrl ignores client select to prevent column-leak",
  withEnv({ SUPABASE_URL: "https://test.supabase.co" }, () => {
    const req = new Request("http://localhost/test?select=*");
    const url = buildProxyUrl(req, baseConfig);
    const parsed = new URL(url);
    assertEquals(parsed.searchParams.get("select"), "id,title");
  }),
);

Deno.test(
  "buildProxyUrl forwards allow-listed params",
  withEnv({ SUPABASE_URL: "https://test.supabase.co" }, () => {
    const req = new Request("http://localhost/test?limit=10&offset=5");
    const config: ProxyConfig = {
      ...baseConfig,
      allowedParams: ["limit", "offset"],
    };
    const url = buildProxyUrl(req, config);
    const parsed = new URL(url);
    assertEquals(parsed.searchParams.get("limit"), "10");
    assertEquals(parsed.searchParams.get("offset"), "5");
  }),
);

Deno.test(
  "buildProxyUrl drops unknown params",
  withEnv({ SUPABASE_URL: "https://test.supabase.co" }, () => {
    const req = new Request("http://localhost/test?limit=10&evil=drop");
    const config: ProxyConfig = { ...baseConfig, allowedParams: ["limit"] };
    const url = buildProxyUrl(req, config);
    assertEquals(url.includes("evil"), false);
  }),
);

Deno.test(
  "buildProxyUrl drops empty allow-listed params",
  withEnv({ SUPABASE_URL: "https://test.supabase.co" }, () => {
    const req = new Request("http://localhost/test?limit=&category=tech");
    const config: ProxyConfig = {
      ...baseConfig,
      allowedParams: ["limit", "category"],
      defaultLimit: 100,
    };
    const url = buildProxyUrl(req, config);
    const parsed = new URL(url);
    // Empty limit dropped → defaultLimit applied
    assertEquals(parsed.searchParams.get("limit"), "100");
    assertEquals(parsed.searchParams.get("category"), "tech");
  }),
);

Deno.test(
  "buildProxyUrl applies maxLimit clamp",
  withEnv({ SUPABASE_URL: "https://test.supabase.co" }, () => {
    const req = new Request("http://localhost/test?limit=999999");
    const config: ProxyConfig = {
      ...baseConfig,
      allowedParams: ["limit"],
      maxLimit: 100,
    };
    const url = buildProxyUrl(req, config);
    const parsed = new URL(url);
    assertEquals(parsed.searchParams.get("limit"), "100");
  }),
);

Deno.test(
  "buildProxyUrl drops NaN limit and uses defaultLimit",
  withEnv({ SUPABASE_URL: "https://test.supabase.co" }, () => {
    const req = new Request("http://localhost/test?limit=NaN");
    const config: ProxyConfig = {
      ...baseConfig,
      allowedParams: ["limit"],
      defaultLimit: 50,
      maxLimit: 100,
    };
    const url = buildProxyUrl(req, config);
    const parsed = new URL(url);
    assertEquals(parsed.searchParams.get("limit"), "50");
  }),
);

Deno.test(
  "buildProxyUrl drops negative offset",
  withEnv({ SUPABASE_URL: "https://test.supabase.co" }, () => {
    const req = new Request("http://localhost/test?offset=-1");
    const config: ProxyConfig = {
      ...baseConfig,
      allowedParams: ["offset"],
    };
    const url = buildProxyUrl(req, config);
    const parsed = new URL(url);
    assertEquals(parsed.searchParams.has("offset"), false);
  }),
);

Deno.test(
  "buildProxyUrl accepts whitelisted order column",
  withEnv({ SUPABASE_URL: "https://test.supabase.co" }, () => {
    const req = new Request("http://localhost/test?order=published_at.desc");
    const config: ProxyConfig = {
      ...baseConfig,
      allowedParams: ["order"],
      allowedOrderColumns: ["published_at"],
    };
    const url = buildProxyUrl(req, config);
    const parsed = new URL(url);
    assertEquals(parsed.searchParams.get("order"), "published_at.desc");
  }),
);

Deno.test(
  "buildProxyUrl drops order on non-whitelisted column",
  withEnv({ SUPABASE_URL: "https://test.supabase.co" }, () => {
    const req = new Request("http://localhost/test?order=secret_col.desc");
    const config: ProxyConfig = {
      ...baseConfig,
      allowedParams: ["order"],
      allowedOrderColumns: ["published_at"],
    };
    const url = buildProxyUrl(req, config);
    const parsed = new URL(url);
    assertEquals(parsed.searchParams.has("order"), false);
  }),
);

Deno.test(
  "buildProxyUrl drops malformed order value",
  withEnv({ SUPABASE_URL: "https://test.supabase.co" }, () => {
    const req = new Request("http://localhost/test?order=published_at-desc");
    const config: ProxyConfig = {
      ...baseConfig,
      allowedParams: ["order"],
      allowedOrderColumns: ["published_at"],
    };
    const url = buildProxyUrl(req, config);
    const parsed = new URL(url);
    assertEquals(parsed.searchParams.has("order"), false);
  }),
);

Deno.test(
  "buildProxyUrl honors nullsfirst/nullslast suffix",
  withEnv({ SUPABASE_URL: "https://test.supabase.co" }, () => {
    const req = new Request(
      "http://localhost/test?order=published_at.desc.nullslast",
    );
    const config: ProxyConfig = {
      ...baseConfig,
      allowedParams: ["order"],
      allowedOrderColumns: ["published_at"],
    };
    const url = buildProxyUrl(req, config);
    const parsed = new URL(url);
    assertEquals(
      parsed.searchParams.get("order"),
      "published_at.desc.nullslast",
    );
  }),
);

Deno.test(
  "buildProxyUrl allows any column when allowedOrderColumns is empty/unset",
  withEnv({ SUPABASE_URL: "https://test.supabase.co" }, () => {
    const req = new Request("http://localhost/test?order=anything.asc");
    const config: ProxyConfig = {
      ...baseConfig,
      allowedParams: ["order"],
    };
    const url = buildProxyUrl(req, config);
    const parsed = new URL(url);
    assertEquals(parsed.searchParams.get("order"), "anything.asc");
  }),
);

Deno.test(
  "buildProxyUrl drops oversized param values",
  withEnv({ SUPABASE_URL: "https://test.supabase.co" }, () => {
    const huge = "x".repeat(500);
    const req = new Request(`http://localhost/test?slug=${huge}`);
    const config: ProxyConfig = {
      ...baseConfig,
      allowedParams: ["slug"],
      maxValueLength: 256,
    };
    const url = buildProxyUrl(req, config);
    const parsed = new URL(url);
    assertEquals(parsed.searchParams.has("slug"), false);
  }),
);

Deno.test(
  "buildProxyUrl produces a canonical (sorted) URL",
  withEnv({ SUPABASE_URL: "https://test.supabase.co" }, () => {
    const req1 = new Request("http://localhost/test?b=2&a=1");
    const req2 = new Request("http://localhost/test?a=1&b=2");
    const config: ProxyConfig = { ...baseConfig, allowedParams: ["a", "b"] };
    const url1 = buildProxyUrl(req1, config);
    const url2 = buildProxyUrl(req2, config);
    assertEquals(url1, url2);
  }),
);

// --- buildCacheKey ---

Deno.test(
  "buildCacheKey is identical for equivalent requests",
  withEnv({ SUPABASE_URL: "https://test.supabase.co" }, () => {
    const r1 = new Request("http://localhost/test?b=2&a=1&junk=zzz");
    const r2 = new Request("http://localhost/test?a=1&b=2");
    const config: ProxyConfig = { ...baseConfig, allowedParams: ["a", "b"] };
    assertEquals(buildCacheKey("foo", r1, config), buildCacheKey("foo", r2, config));
  }),
);

Deno.test(
  "buildCacheKey carries the prefix",
  withEnv({ SUPABASE_URL: "https://test.supabase.co" }, () => {
    const req = new Request("http://localhost/test");
    const key = buildCacheKey("sources", req, baseConfig);
    assert(key.startsWith("sources:"));
  }),
);

// --- tooLong ---

Deno.test("tooLong returns null for normal requests", () => {
  const req = new Request("http://localhost/test?a=1");
  assertEquals(tooLong(req), null);
});

Deno.test("tooLong returns 414 for oversized search string", async () => {
  const big = "a".repeat(MAX_QUERY_STRING_LEN + 1);
  const req = new Request(`http://localhost/test?q=${big}`);
  const resp = tooLong(req, { "X-Foo": "bar" });
  assert(resp !== null);
  assertEquals(resp!.status, 414);
  assertEquals(resp!.headers.get("X-Foo"), "bar");
  const body = await resp!.json();
  assertEquals(body.error, "Request URI too long");
});

// --- fetchFromSupabase ---

Deno.test(
  "fetchFromSupabase throws when SUPABASE_ANON_KEY not set",
  withEnv(
    { SUPABASE_URL: "https://test.supabase.co", SUPABASE_ANON_KEY: undefined },
    async () => {
      const req = new Request("http://localhost/test");
      await assertRejects(
        () => fetchFromSupabase(req, baseConfig),
        Error,
        "SUPABASE_ANON_KEY",
      );
    },
  ),
);

Deno.test(
  "fetchFromSupabase returns data on success",
  withEnv(
    {
      SUPABASE_URL: "https://test.supabase.co",
      SUPABASE_ANON_KEY: "test-key",
    },
    async () => {
      const originalFetch = globalThis.fetch;
      try {
        globalThis.fetch = (_input: string | URL | Request, _init?: RequestInit) =>
          Promise.resolve(
            new Response('[{"id":"1"}]', {
              status: 200,
              headers: { "Content-Type": "application/json" },
            }),
          );
        const result = await fetchFromSupabase(
          new Request("http://localhost/test"),
          baseConfig,
        );
        assertEquals(result.status, 200);
        assertEquals(result.data, '[{"id":"1"}]');
      } finally {
        globalThis.fetch = originalFetch;
      }
    },
  ),
);

Deno.test(
  "fetchFromSupabase forwards content-range header",
  withEnv(
    {
      SUPABASE_URL: "https://test.supabase.co",
      SUPABASE_ANON_KEY: "test-key",
    },
    async () => {
      const originalFetch = globalThis.fetch;
      try {
        globalThis.fetch = (_input: string | URL | Request, _init?: RequestInit) =>
          Promise.resolve(
            new Response("[]", {
              status: 200,
              headers: {
                "Content-Type": "application/json",
                "Content-Range": "0-9/100",
              },
            }),
          );
        const result = await fetchFromSupabase(
          new Request("http://localhost/test"),
          baseConfig,
        );
        assertEquals(result.contentRange, "0-9/100");
      } finally {
        globalThis.fetch = originalFetch;
      }
    },
  ),
);

Deno.test(
  "fetchFromSupabase proxies non-200 status",
  withEnv(
    {
      SUPABASE_URL: "https://test.supabase.co",
      SUPABASE_ANON_KEY: "test-key",
    },
    async () => {
      const originalFetch = globalThis.fetch;
      try {
        globalThis.fetch = (_input: string | URL | Request, _init?: RequestInit) =>
          Promise.resolve(
            new Response('{"error":"bad request"}', { status: 400 }),
          );
        const result = await fetchFromSupabase(
          new Request("http://localhost/test"),
          baseConfig,
        );
        assertEquals(result.status, 400);
      } finally {
        globalThis.fetch = originalFetch;
      }
    },
  ),
);

Deno.test(
  "fetchFromSupabase sends Prefer count=estimated header",
  withEnv(
    {
      SUPABASE_URL: "https://test.supabase.co",
      SUPABASE_ANON_KEY: "test-key",
    },
    async () => {
      const originalFetch = globalThis.fetch;
      let captured: Headers | undefined;
      try {
        globalThis.fetch = (input: string | URL | Request, init?: RequestInit) => {
          if (init?.headers) captured = new Headers(init.headers as HeadersInit);
          else if (input instanceof Request) captured = input.headers;
          return Promise.resolve(new Response("[]", { status: 200 }));
        };
        await fetchFromSupabase(
          new Request("http://localhost/test"),
          baseConfig,
        );
        assertEquals(captured?.get("Prefer"), "count=estimated");
      } finally {
        globalThis.fetch = originalFetch;
      }
    },
  ),
);

Deno.test("isUuidFilter accepts only UUID value(s)", () => {
  assert(isUuidFilter("eq.11111111-1111-1111-1111-111111111111"));
  assert(isUuidFilter("11111111-1111-1111-1111-111111111111")); // no operator
  assert(isUuidFilter(
    "in.(11111111-1111-1111-1111-111111111111,22222222-2222-2222-2222-222222222222)",
  ));
  assert(!isUuidFilter("eq.not-a-uuid"));
  assert(!isUuidFilter("eq.")); // empty value
  assert(!isUuidFilter("eq.(11111111-1111-1111-1111-111111111111,bad)"));
});

Deno.test("isSlugFilter accepts only kebab-case slug value(s)", () => {
  assert(isSlugFilter("eq.bbc-news"));
  assert(isSlugFilter("eq.20-minutos"));
  assert(isSlugFilter("g1"));
  assert(!isSlugFilter("eq.has spaces"));
  assert(!isSlugFilter("eq.under_score"));
  assert(!isSlugFilter("eq." + "a".repeat(129))); // length cap
});

Deno.test("isBooleanFilter accepts only true/false value(s)", () => {
  assert(isBooleanFilter("eq.true"));
  assert(isBooleanFilter("is.false"));
  assert(isBooleanFilter("true"));
  assert(!isBooleanFilter("eq.maybe"));
  assert(!isBooleanFilter("eq.1"));
});

Deno.test("isCacheableResult rejects empty result sets", () => {
  assert(isCacheableResult('[{"id":"x"}]'));
  assert(isCacheableResult('{"sources":[]}')); // non-array object still cacheable
  assert(!isCacheableResult("[]"));
  assert(!isCacheableResult("  []  "));
  assert(!isCacheableResult(""));
});

Deno.test("paramValidators drop invalid values from the upstream URL", () => {
  const cfg: ProxyConfig = {
    table: "sources",
    allowedParams: ["id", "is_active"],
    defaultSelect: "id,name",
    paramValidators: { id: isUuidFilter, is_active: isBooleanFilter },
  };
  const prevUrl = Deno.env.get("SUPABASE_URL");
  Deno.env.set("SUPABASE_URL", "https://t.supabase.co");
  try {
    const url = buildProxyUrl(
      new Request("http://localhost/x?id=not.a.uuid&is_active=eq.maybe"),
      cfg,
    );
    assert(!url.includes("id="), `invalid id should be dropped: ${url}`);
    assert(!url.includes("is_active="), `invalid is_active should be dropped: ${url}`);

    const ok = buildProxyUrl(
      new Request(
        "http://localhost/x?id=eq.11111111-1111-1111-1111-111111111111&is_active=eq.true",
      ),
      cfg,
    );
    assertStringIncludes(ok, "id=eq.11111111-1111-1111-1111-111111111111");
    assertStringIncludes(ok, "is_active=eq.true");
  } finally {
    if (prevUrl) Deno.env.set("SUPABASE_URL", prevUrl);
    else Deno.env.delete("SUPABASE_URL");
  }
});

Deno.test("isLanguageFilter accepts only ISO 639-1 code(s)", () => {
  assert(isLanguageFilter("eq.en"));
  assert(isLanguageFilter("eq.pt"));
  assert(isLanguageFilter("in.(en,pt,es)"));
  assert(!isLanguageFilter("eq.english")); // full name not allowed
  assert(!isLanguageFilter("eq.en-US")); // region suffix rejected
  assert(!isLanguageFilter("eq.")); // empty value
});

Deno.test("paramValidators drop invalid language from the upstream URL", () => {
  const cfg: ProxyConfig = {
    table: "articles",
    allowedParams: ["language"],
    defaultSelect: "id,title",
    paramValidators: { language: isLanguageFilter },
  };
  const prevUrl = Deno.env.get("SUPABASE_URL");
  Deno.env.set("SUPABASE_URL", "https://t.supabase.co");
  try {
    const url = buildProxyUrl(
      new Request("http://localhost/x?language=eq.english"),
      cfg,
    );
    assert(!url.includes("language="), `invalid language should be dropped: ${url}`);

    const ok = buildProxyUrl(
      new Request("http://localhost/x?language=eq.en"),
      cfg,
    );
    assertStringIncludes(ok, "language=eq.en");
  } finally {
    if (prevUrl) Deno.env.set("SUPABASE_URL", prevUrl);
    else Deno.env.delete("SUPABASE_URL");
  }
});
