/**
 * Articles API Endpoint
 *
 * Returns a paginated list of articles from the `articles_with_source` view.
 *
 * ## Query Parameters
 * - `limit` (default 100, max 100)
 * - `offset` (pagination)
 * - `source_slug` (e.g., `source_slug=eq.bbc-news`)
 * - `category_slug` (e.g., `category_slug=eq.technology`)
 * - `language` (e.g., `language=eq.en`)
 * - `media_type` (e.g., `media_type=eq.podcast`)
 * - `published_at` (e.g., `published_at=gte.2024-01-01`)
 * - `order` (only `published_at.asc|desc[.nullsfirst|.nullslast]`)
 * - `id` (e.g., `id=eq.<uuid>`)
 *
 * ## Response
 * JSON array of article objects with fields:
 * - `id`, `title`, `summary`, `url`, `image_url`
 * - `published_at`, `language`, `source_name`, `source_slug`, `category_name`, `category_slug`
 * - `media_type`, `media_url`, `media_duration`, `media_mime_type`
 *
 * Single-article requests (`?id=eq.<uuid>`) additionally return `content`,
 * `thumbnail_url` and `author` — the article-detail payload. See
 * `DETAIL_SELECT` for why those columns stay off the list response.
 *
 * ## Caching
 * - Cache-Control: 15 min fresh, 30 min stale-while-revalidate
 * - ETag/304 supported, but only for 200 OK upstream responses
 *
 * @module api-articles
 */
import { corsHeaders, handleCors } from "../_shared/cors.ts";
import { CacheDurations, cacheHeaders } from "../_shared/cache.ts";
import { checkConditionalRequest, generateETag } from "../_shared/etag.ts";
import {
  fetchFromSupabase,
  isLanguageFilter,
  isSingleUuidEqFilter,
  isUpstreamSuccess,
  isUuidFilter,
  type ProxyConfig,
  tooLong,
} from "../_shared/supabase-proxy.ts";

const LIST_SELECT =
  "id,title,summary,url,image_url,published_at,language,source_name,source_slug,category_name,category_slug,media_type,media_url,media_duration,media_mime_type";

/**
 * Article-detail projection. `content` is the full extracted body (up to 200K
 * runes before compression), so it must never ride along on a 100-row list
 * response — that's why `LIST_SELECT` omits it. The client's own `select`
 * param is always discarded by the proxy (column-leak guard), so a detail
 * fetch has to be recognised server-side: exactly the `?id=eq.<uuid>` shape.
 *
 * Note `content` is NULL for articles older than the prune window
 * (migration 032); clients must fall back to `summary`.
 */
const DETAIL_SELECT = `${LIST_SELECT},content,thumbnail_url,author`;

const config: ProxyConfig = {
  table: "articles_with_source",
  allowedParams: [
    "id",
    "source_slug",
    "category_slug",
    "language",
    "media_type",
    "order",
    "limit",
    "offset",
    "published_at",
  ],
  defaultSelect: LIST_SELECT,
  defaultLimit: 100,
  maxLimit: 100,
  allowedOrderColumns: ["published_at"],
  paramValidators: {
    id: isUuidFilter,
    language: isLanguageFilter,
  },
};

const detailConfig: ProxyConfig = { ...config, defaultSelect: DETAIL_SELECT };

/**
 * Only `id=eq.<uuid>` means "fetch one article" → detail projection.
 *
 * Deliberately stricter than `paramValidators.id`: `isUuidFilter` accepts any
 * operator, so `id=in.(…100 uuids)` or `id=neq.<uuid>` would otherwise return
 * the full `content` body for up to `limit` rows — the multi-row heavy payload
 * this split exists to prevent. Those shapes stay valid filters; they just get
 * the list projection.
 */
function configFor(req: Request): ProxyConfig {
  const id = new URL(req.url).searchParams.get("id");
  return id !== null && isSingleUuidEqFilter(id) ? detailConfig : config;
}

export async function handler(req: Request): Promise<Response> {
  const corsResponse = handleCors(req);
  if (corsResponse) return corsResponse;

  if (req.method !== "GET") {
    return new Response(JSON.stringify({ error: "Method not allowed" }), {
      status: 405,
      headers: { ...corsHeaders, "Content-Type": "application/json" },
    });
  }

  const oversized = tooLong(req, corsHeaders);
  if (oversized) return oversized;

  // A malformed `id` is dropped by the proxy's validator, which would turn a
  // single-article lookup into an unfiltered list — and the client takes the
  // first row, i.e. silently the wrong article. Answer with an empty set.
  // Uses the permissive `isUuidFilter` on purpose: it mirrors exactly what
  // `paramValidators.id` will accept, so this fires for the values that would
  // actually be dropped. Shapes like `in.(…)` survive validation and stay
  // honest filters — they just don't get the detail projection.
  const rawId = new URL(req.url).searchParams.get("id");
  if (rawId !== null && rawId !== "" && !isUuidFilter(rawId)) {
    return new Response(JSON.stringify([]), {
      status: 200,
      headers: {
        ...corsHeaders,
        ...cacheHeaders(CacheDurations.ARTICLES),
        "Content-Type": "application/json",
      },
    });
  }

  try {
    const result = await fetchFromSupabase(req, configFor(req));

    // ETag/304 only on success — never cache or echo error bodies (which would
    // pin clients to a stale error state on `If-None-Match` replay, and may
    // leak PostgREST schema details). All unsuccessful upstream responses are
    // returned with a generic JSON error body.
    if (!isUpstreamSuccess(result.status)) {
      return new Response(
        JSON.stringify({ error: "upstream error" }),
        {
          status: result.status,
          headers: { ...corsHeaders, "Content-Type": "application/json" },
        },
      );
    }

    const etag = await generateETag(result.data);
    const conditional = checkConditionalRequest(req, etag);
    if (conditional) {
      return new Response(null, {
        status: 304,
        headers: {
          ...corsHeaders,
          ...cacheHeaders(CacheDurations.ARTICLES),
          ETag: etag,
        },
      });
    }

    const headers: Record<string, string> = {
      ...corsHeaders,
      ...cacheHeaders(CacheDurations.ARTICLES),
      "Content-Type": "application/json",
      ETag: etag,
    };
    if (result.contentRange) {
      headers["Content-Range"] = result.contentRange;
    }

    return new Response(result.data, {
      status: result.status,
      headers,
    });
  } catch (error) {
    console.error("Error fetching articles:", error);
    return new Response(
      JSON.stringify({ error: "Internal server error" }),
      {
        status: 500,
        headers: { ...corsHeaders, "Content-Type": "application/json" },
      },
    );
  }
}

if (import.meta.main) {
  Deno.serve(handler);
}
