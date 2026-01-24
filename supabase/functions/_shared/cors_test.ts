import {
  assertEquals,
  assertExists,
} from "https://deno.land/std@0.208.0/assert/mod.ts";
import { corsHeaders, handleCors } from "./cors.ts";

Deno.test("corsHeaders has Access-Control-Allow-Origin", () => {
  assertExists(corsHeaders["Access-Control-Allow-Origin"]);
  assertEquals(corsHeaders["Access-Control-Allow-Origin"], "*");
});

Deno.test("corsHeaders allows GET and OPTIONS methods", () => {
  assertEquals(corsHeaders["Access-Control-Allow-Methods"], "GET, OPTIONS");
});

Deno.test("corsHeaders allows required headers", () => {
  const allowedHeaders = corsHeaders["Access-Control-Allow-Headers"];
  assertEquals(allowedHeaders.includes("authorization"), true);
  assertEquals(allowedHeaders.includes("apikey"), true);
  assertEquals(allowedHeaders.includes("content-type"), true);
  assertEquals(allowedHeaders.includes("if-none-match"), true);
});

Deno.test("corsHeaders exposes etag and cache-control", () => {
  const exposed = corsHeaders["Access-Control-Expose-Headers"];
  assertEquals(exposed.includes("etag"), true);
  assertEquals(exposed.includes("cache-control"), true);
});

Deno.test("handleCors returns null for GET request", () => {
  const req = new Request("https://example.com/api", { method: "GET" });
  const response = handleCors(req);
  assertEquals(response, null);
});

Deno.test("handleCors returns null for POST request", () => {
  const req = new Request("https://example.com/api", { method: "POST" });
  const response = handleCors(req);
  assertEquals(response, null);
});

Deno.test("handleCors returns 204 for OPTIONS request", () => {
  const req = new Request("https://example.com/api", { method: "OPTIONS" });
  const response = handleCors(req);
  assertExists(response);
  assertEquals(response!.status, 204);
});

Deno.test("handleCors OPTIONS response has CORS headers", () => {
  const req = new Request("https://example.com/api", { method: "OPTIONS" });
  const response = handleCors(req)!;
  assertEquals(response.headers.get("Access-Control-Allow-Origin"), "*");
  assertEquals(
    response.headers.get("Access-Control-Allow-Methods"),
    "GET, OPTIONS"
  );
});

Deno.test("handleCors OPTIONS response has no body", async () => {
  const req = new Request("https://example.com/api", { method: "OPTIONS" });
  const response = handleCors(req)!;
  const body = await response.text();
  assertEquals(body, "");
});
