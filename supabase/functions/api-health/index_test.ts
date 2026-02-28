import { assertEquals } from "https://deno.land/std@0.208.0/assert/mod.ts";
import { handler } from "./index.ts";

Deno.test("GET returns 200 with status and timestamp", async () => {
  const req = new Request("http://localhost/api-health");
  const res = handler(req);

  assertEquals(res.status, 200);
  assertEquals(res.headers.get("Content-Type"), "application/json");

  const body = await res.json();
  assertEquals(body.status, "ok");
  assertEquals(typeof body.timestamp, "string");
  // Verify timestamp is valid ISO 8601
  const parsed = new Date(body.timestamp);
  assertEquals(isNaN(parsed.getTime()), false);
});

Deno.test("GET returns no-store cache header", () => {
  const req = new Request("http://localhost/api-health");
  const res = handler(req);

  assertEquals(res.status, 200);
  assertEquals(res.headers.get("Cache-Control"), "no-store");
});

Deno.test("OPTIONS returns CORS 204", () => {
  const req = new Request("http://localhost/api-health", {
    method: "OPTIONS",
  });
  const res = handler(req);
  assertEquals(res.status, 204);
  assertEquals(res.headers.get("Access-Control-Allow-Origin"), "*");
});

Deno.test("non-GET returns 405", async () => {
  const req = new Request("http://localhost/api-health", {
    method: "POST",
  });
  const res = handler(req);
  assertEquals(res.status, 405);
  const body = await res.json();
  assertEquals(body.error, "Method not allowed");
});
