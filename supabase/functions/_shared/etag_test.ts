import {
  assertEquals,
  assertNotEquals,
} from "https://deno.land/std@0.208.0/assert/mod.ts";
import { generateETag, checkConditionalRequest } from "./etag.ts";

Deno.test("generateETag produces quoted string", async () => {
  const etag = await generateETag("test data");
  assertEquals(etag.startsWith('"'), true);
  assertEquals(etag.endsWith('"'), true);
});

Deno.test("generateETag is deterministic (same input = same output)", async () => {
  const etag1 = await generateETag("same content");
  const etag2 = await generateETag("same content");
  assertEquals(etag1, etag2);
});

Deno.test("generateETag produces different values for different content", async () => {
  const etag1 = await generateETag("content A");
  const etag2 = await generateETag("content B");
  assertNotEquals(etag1, etag2);
});

Deno.test("generateETag produces 18 char string (quotes + 16 hex)", async () => {
  const etag = await generateETag("test");
  // Format: "xxxxxxxxxxxxxxxx" (2 quotes + 16 hex chars)
  assertEquals(etag.length, 18);
});

Deno.test("generateETag content is hex characters", async () => {
  const etag = await generateETag("test");
  const content = etag.slice(1, -1); // Remove quotes
  const hexRegex = /^[0-9a-f]+$/;
  assertEquals(hexRegex.test(content), true);
});

Deno.test("generateETag handles empty string", async () => {
  const etag = await generateETag("");
  assertEquals(etag.length, 18);
  assertEquals(etag.startsWith('"'), true);
  assertEquals(etag.endsWith('"'), true);
});

Deno.test("generateETag handles unicode content", async () => {
  const etag = await generateETag("Hello 世界 🌍");
  assertEquals(etag.length, 18);
});

Deno.test("generateETag handles large content", async () => {
  const largeContent = "x".repeat(100000);
  const etag = await generateETag(largeContent);
  assertEquals(etag.length, 18);
});

Deno.test("checkConditionalRequest returns null when no If-None-Match header", () => {
  const req = new Request("https://example.com/api");
  const response = checkConditionalRequest(req, '"abc123"');
  assertEquals(response, null);
});

Deno.test("checkConditionalRequest returns null when ETags differ", () => {
  const req = new Request("https://example.com/api", {
    headers: { "If-None-Match": '"old-etag-value"' },
  });
  const response = checkConditionalRequest(req, '"new-etag-value"');
  assertEquals(response, null);
});

Deno.test("checkConditionalRequest returns 304 when ETags match", () => {
  const etag = '"matching-etag123"';
  const req = new Request("https://example.com/api", {
    headers: { "If-None-Match": etag },
  });
  const response = checkConditionalRequest(req, etag);
  assertNotEquals(response, null);
  assertEquals(response!.status, 304);
});

Deno.test("checkConditionalRequest 304 response includes ETag header", () => {
  const etag = '"test-etag-value"';
  const req = new Request("https://example.com/api", {
    headers: { "If-None-Match": etag },
  });
  const response = checkConditionalRequest(req, etag)!;
  assertEquals(response.headers.get("ETag"), etag);
});

Deno.test("checkConditionalRequest 304 response has no body", async () => {
  const etag = '"test-etag"';
  const req = new Request("https://example.com/api", {
    headers: { "If-None-Match": etag },
  });
  const response = checkConditionalRequest(req, etag)!;
  const body = await response.text();
  assertEquals(body, "");
});

Deno.test("checkConditionalRequest case-sensitive ETag comparison", () => {
  const req = new Request("https://example.com/api", {
    headers: { "If-None-Match": '"ABCD1234"' },
  });
  // Should not match - ETags are case-sensitive
  const response = checkConditionalRequest(req, '"abcd1234"');
  assertEquals(response, null);
});
