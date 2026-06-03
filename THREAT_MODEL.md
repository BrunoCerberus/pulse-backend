# Threat Model

This is the authoritative threat model for Pulse Backend. It follows
[Shostack's four questions](https://shostack.org/resources/threat-modeling):
*what are we building, what can go wrong, what are we doing about it,* and
*did we do a good job.* It is committed to the repo and updated as the code
changes — when you add an entry point, a dependency, or a privileged code path,
update the relevant section here in the same PR.

It complements, and does not replace:

- [`SECURITY.md`](SECURITY.md) — vulnerability disclosure policy + triage rubric.
- [`docs/privacy.md`](docs/privacy.md) and the LGPD/GDPR/CCPA conformance docs —
  the data-protection posture (no end-user PII).
- [`PATCHING.md`](PATCHING.md) — how a confirmed finding gets fixed.

> **For automated reviewers (Claude Code and the security-review action):** treat
> every RSS feed, article page, and enclosure as **hostile, attacker-controlled
> input**. The 136 configured publishers are not trusted. Pay special attention
> to changes touching the SSRF-aware transport, the parser's input limits and
> sanitizers, the Edge Function request guards, and the Supabase
> `SECURITY DEFINER` / RLS / GRANT boundary. Do not treat this as a checklist —
> it is context for where the risk concentrates.

---

## 1. What are we building?

Pulse Backend is a self-hosted news-aggregation backend with no end-user
accounts. The data flow is one-directional:

```
                  ┌─────────────────── TRUST BOUNDARY ───────────────────┐
                  │  136 external RSS publishers (UNTRUSTED)              │
                  └───────────────────────┬──────────────────────────────┘
                                          │ RSS/Atom XML, article HTML,
                                          │ og:image HTML, media enclosures
                                          ▼
   GitHub Actions (scheduled) ──▶  Go RSS worker (rss-worker/)
                                   ├─ fetch via SSRF-aware transport
                                   ├─ parse (gofeed) + sanitize + cap
                                   ├─ enrich: og:image, full content
                                   └─ batch insert (service-role key)
                                          │
                  ┌─────────────────── TRUST BOUNDARY ───────────────────┐
                  │  Supabase / PostgreSQL                                │
                  │  RLS + column grants + SECURITY DEFINER functions     │
                  └───────────────────────┬──────────────────────────────┘
                                          │ anon-key reads through views
                                          ▼
                           Edge Functions (caching proxy, public)
                                          │ public, read-only HTTP
                                          ▼
                                   Pulse iOS app  (UNTRUSTED clients)
```

**Components:** the Go worker (`rss-worker/`), the PostgreSQL schema
(`supabase/migrations/`), the Deno/TypeScript Edge Functions
(`supabase/functions/`), and the GitHub Actions automation (`.github/workflows/`).

### Entry points (where untrusted data crosses a boundary)

| # | Entry point | Untrusted input | Reached by |
|---|-------------|-----------------|------------|
| E1 | RSS/Atom feed fetch | Feed XML body, HTTP redirect targets, response headers (ETag/Last-Modified) | `parser.ParseFeed` via the rate-limited SSRF-aware client |
| E2 | og:image enrichment | Article-page HTML `<head>`, the `og:image`/`twitter:image` URL | `parser` og:image extractor |
| E3 | Full-content enrichment | Article-page HTML body | `parser` content extractor (go-readability) |
| E4 | Media enclosures | Enclosure URL, MIME type, iTunes duration string | `itemToArticle` media extraction |
| E5 | Public Edge API | Query string + headers on `/api-articles`, `/api-search`, `/api-sources`, `/api-categories`, `/api-health`, `/api-source-health` | iOS app / any internet client |
| E6 | Supabase REST (anon key) | PostgREST query params, `.textSearch` against `articles_with_source` | iOS app / any internet client |
| E7 | CI / supply chain | Dependency updates, third-party GitHub Actions, fork-PR diffs/titles | Dependabot, contributors |

### Assets (what an attacker would want)

- **A1 — `SUPABASE_SERVICE_ROLE_KEY`** (full DB write). Lives only in GitHub
  Actions secrets and the worker's process env. **Highest-value asset.**
- **A2 — Database integrity.** `articles`, `sources` (incl. circuit-breaker
  state), `categories`, `fetch_logs`. No PII, but corruption/poisoning degrades
  the product and could push malicious URLs to iOS clients.
- **A3 — `CLAUDE_CODE_OAUTH_TOKEN` / `CLAUDE_API_KEY`** (CI review bots).
- **A4 — Availability** of the worker and the Edge API (free-tier quotas:
  Supabase DB size, GitHub Actions minutes).
- **A5 — Deploy credentials** (`SUPABASE_ACCESS_TOKEN`, `SUPABASE_PROJECT_REF`,
  `SUPABASE_DB_PASSWORD`) — scoped to the `production` GitHub Environment.

### Trust boundaries (what is trusted vs. not)

1. **Publisher content (E1–E4) is untrusted.** A feed can be hostile or a
   trusted publisher can be compromised. Everything ingested is adversarial.
2. **The public Edge API and anon-key DB surface (E5–E6) are untrusted
   callers.** Reads only; no caller is authenticated as a person.
3. **`anon` / `authenticated` Postgres roles are untrusted; `service_role` is
   trusted.** This is the load-bearing privilege boundary, enforced by
   GRANT/REVOKE + RLS, with `SECURITY DEFINER` + a JWT-claim caller gate as
   defence in depth.
4. **CI secrets are trusted; fork-PR contributions are not.** Fork PRs carry
   attacker-controlled diffs/titles/bodies and must never receive write-scoped
   tokens or run prompt-injection-susceptible bots.
5. **Supabase and GitHub are trusted subprocessors** (see `docs/ropa.md`).

---

## 2. What can go wrong?

Threats are organized by the entry point they exploit. The right-hand column
points to the mitigating control (see §3). "Cluster" tags group threats into
the vulnerability classes worth targeting during discovery.

### Against the worker's ingestion path (E1–E4)

| Threat | Cluster | Mitigation |
|--------|---------|------------|
| Feed/redirect points at `localhost`/RFC-1918/link-local/cloud-metadata to reach internal services (SSRF) | **SSRF** | C-SSRF |
| Hostile DNS rebinds between validation and connect (TOCTOU) | **SSRF** | C-SSRF |
| og:image URL coerces iOS clients to probe internal addresses | **SSRF** | C-OGIMG |
| Multi-gigabyte feed / page body exhausts worker memory (DoS) | **DoS** | C-LIMITS |
| Feed-supplied MIME type smuggles CRLF / extra headers | **Injection** | C-SANITIZE |
| Oversized/overflowing media duration corrupts numeric fields | **DoS** | C-SANITIZE |
| Control / bidi-override codepoints spoof rendering on iOS | **Injection** | C-SANITIZE |
| `javascript:`/`data:`/`file:` URLs propagate to clients | **Injection** | C-URLSAFE |
| Query-reorder / fragment tricks bypass dedup → duplicate flooding | **DoS** | C-CANON |
| Far-future `published_at` pins an article to the top forever | **Logic** | C-CLAMP |
| One hostile feed hammers a publisher through us (amplification) | **DoS** | C-RATELIMIT |
| Dead/hostile feed wastes every fetch cycle | **Availability** | C-CIRCUIT |

### Against the database (E5–E6, and the worker's writes)

| Threat | Cluster | Mitigation |
|--------|---------|------------|
| `anon` reads columns it should not see (`url_hash`, backfill state) | **Data exposure** | C-GRANT, C-VIEW |
| `search_path` hijack redirects a built-in inside a `SECURITY DEFINER` fn | **Privilege escalation** | C-DEFINER |
| `anon`/`authenticated` invokes a privileged write function | **Privilege escalation** | C-GRANT, C-CALLERGATE |
| Unbounded / pathological search query exhausts the DB (DoS) | **DoS** | C-SEARCHCAP |
| `SETOF articles` projection leaks future columns automatically | **Data exposure** | C-VIEW |
| Oversized batch RPC array amplifies per-call work | **DoS** | C-BATCHCAP |

### Against CI & supply chain (E7)

| Threat | Cluster | Mitigation |
|--------|---------|------------|
| Fork PR coerces a review bot into auto-approving via prompt injection | **Supply chain** | C-FORKGATE |
| Mutable action ref (`@main`) is repointed to malicious code | **Supply chain** | C-PIN |
| Over-broad workflow token is abused after a step is compromised | **Supply chain** | C-LEASTPRIV |
| A vulnerable transitive dependency ships to production | **Supply chain** | C-DEPSCAN |
| A leaked secret lands in git history | **Secret exposure** | C-SECRETSCAN |
| A PR silently introduces PII processing | **Privacy** | C-CONFORMANCE |

### Clustered past vulnerability classes (from git history)

These have actually been found and fixed here, so discovery should keep
revisiting them — they are this codebase's recurring shapes:

- **SSRF / DNS-rebinding** — hardened in `internal/httputil` (resolve-once,
  dial-the-literal, redirect re-validation).
- **`SECURITY DEFINER` `search_path` escalation** — migration 027 rebuilt all
  five write functions with `SET search_path = ''` + fully-qualified refs.
- **Dead caller-gate (`CURRENT_USER` vs `SESSION_USER`)** — migration 033
  replaced an ineffective check with the working `request.jwt.claims` pattern.
- **Column leakage through a view** (`url_hash`) — migration 027 switched
  `articles_with_source` to an explicit projection and column-level grants.
- **Over-broad initial RLS** — migration 005 dropped anon write policies.
- **Parser abuse** — feed-supplied MIME CRLF, length/overflow, bidi/control
  codepoints; capped and sanitized in the parser.
- **Dependency CVEs** — e.g. the `golang.org/x/net` bumps caught by
  govulncheck/Dependabot.

---

## 3. What are we doing about it?

Each control is referenced by the tag used above. Locations name the file/symbol
rather than line numbers so this stays accurate as code moves.

### Worker / ingestion controls

- **C-SSRF** — `internal/httputil/transport.go`: `SafeTransport`'s
  `SecureDialContext` resolves the host once, rejects loopback / RFC-1918 /
  link-local (169.254/16) / multicast / unspecified IPs via `IsForbiddenIP`,
  then dials the resolved literal so DNS can't rebind. `IsForbiddenIP` also
  denies an explicit `forbiddenCIDRs` list the stdlib classifiers miss: CGNAT
  (100.64/10), the NAT64 / 6to4 / Teredo IPv4↔IPv6 translation prefixes (which
  could otherwise wrap 169.254.169.254), and benchmarking / documentation space.
  `ValidateSSRFTarget` pre-flights scheme + host. Redirects are re-validated.
- **C-OGIMG** — `internal/parser/ogimage.go`: `isAcceptableOGImage` rejects
  control chars, non-`http(s)` schemes, empty hosts, and forbidden IP literals
  before an og:image URL is stored; the fetch itself uses `SafeTransport`.
- **C-LIMITS** — body caps via `io.LimitReader`: feed body `MaxFeedBodyBytes`
  (50 MB), content extraction (5 MB), og:image `<head>` (100 KB); per-field rune
  caps (title 500 / summary 4096 / content 200K / author 200 / URL 2048).
- **C-SANITIZE** — `internal/parser/parser.go`: `sanitizeText` strips C0/C1
  control + bidi-override codepoints; `sanitizeMIMEType` enforces a tight MIME
  regex (no CRLF); `parseDuration` bounds each part before combining (so the
  `hours*3600` multiply can't wrap to a bogus positive value) and `parseSafeInt`
  guards per-part overflow; duration capped at 24 h.
- **C-URLSAFE** — `isSafeArticleURL` / `isSafeMediaURL` / `isAcceptableOGImage`
  reject non-`http(s)` schemes, empty hosts, control / bidi-override codepoints
  (`urlHasUnsafeRune`), and over-`maxURLLen` URLs for article, media, thumbnail,
  and og:image fields.
- **C-CANON** — `canonicalizeURL` drops the fragment, lowercases scheme/host,
  sorts query keys before SHA-256 hashing, so dedup can't be bypassed.
- **C-CLAMP** — `clampPublishedDate` bounds `published_at` to `[now-10y, now+1h]`.
- **C-RATELIMIT** — `RateLimitingTransport` applies a per-host token bucket
  (default 2 rps / burst 5) to all user-content clients.
- **C-CIRCUIT** — per-source circuit breaker (`consecutive_failures`,
  `circuit_open_until`, exponential backoff capped at 24 h); open-circuit sources
  are excluded at the DB query layer.

### Database controls

- **C-GRANT** — column-level `GRANT SELECT` on `articles` exposes only the safe
  subset to `anon`/`authenticated`; `url_hash` and backfill state are
  service-role only. The same pattern restricts `sources` to its public set —
  operational / circuit-breaker columns are service-role only (migration 034).
  `fetch_logs` is fully revoked from anon.
- **C-VIEW** — `articles_with_source` is an explicit projection with
  `security_invoker=on`; `source_health` is revoked from anon (migration 027).
- **C-DEFINER** — every `SECURITY DEFINER` function pins `SET search_path = ''`
  with fully-qualified references (migration 027).
- **C-CALLERGATE** — the five write functions check `request.jwt.claims->>'role'`
  for `service_role` (or a direct `postgres` session) — defence in depth behind
  the GRANT boundary (migration 033).
- **C-SEARCHCAP** — `search_articles` rejects empty/whitespace/>200-char queries
  and runs under `statement_timeout = '3s'` with an explicit projection.
- **C-BATCHCAP** — `bump_backfill_attempts` rejects arrays over 10K entries.
- The SQL invariants in `supabase/tests/security_invariants.sql` assert
  C-GRANT/C-VIEW/C-DEFINER/C-CALLERGATE/C-SEARCHCAP on every migration run.

### CI / supply-chain controls

- **C-FORKGATE** — review bots run only for `OWNER`/`MEMBER`/`COLLABORATOR`
  PRs; SARIF-upload steps skip on fork PRs.
- **C-PIN** — third-party actions are pinned to commit SHAs; scanner versions
  pinned via env.
- **C-LEASTPRIV** — workflows default to `contents: read`; jobs widen only as
  needed.
- **C-DEPSCAN** — govulncheck (PR + weekly), Trivy (vuln/secret/misconfig),
  Dependency Review on PRs, CodeQL (Go + TS), Dependabot, SBOM.
- **C-SECRETSCAN** — gitleaks (full history) + TruffleHog (verified).
- **C-CONFORMANCE** — LGPD/GDPR/CCPA workflows ban PII patterns, enforce the
  table/column allowlists, retention literal, and RLS-still-on invariant.

---

## 4. Did we do a good job?

### Assumptions worth restating

- The `author` byline is public-record journalist attribution under the
  journalism exemption — **not** end-user PII (see `SECURITY.md` / `docs/`).
- The service-role key is only ever present in CI secrets and the worker
  process. If it leaks, **A1 is fully compromised** — rotate immediately.
- Supabase enforces RLS and PostgREST sets `request.jwt.claims` per request.
- The worker handles **no inbound user requests** — there is no client-IP code
  path in `rss-worker/` (asserted by the conformance workflows).
- **The Edge Functions are a *narrowing* proxy over a directly-reachable anon
  PostgREST surface** (the anon key is public). They can only reduce surface
  (forced `select`, length caps, order allow-list, limit cap), never widen it —
  so PostgREST operator-injection via forwarded params (`id=not.is.null`,
  `language=ilike.*`) is harmless **today** because every filterable column is
  already anon-`SELECT`able directly. **If this ever changes** — PostgREST locked
  behind the Edge layer, or anon column-grants tightened so the proxy becomes the
  *only* path to some data — `buildProxyUrl` must validate filter *values* (not
  just length), or operator-injection becomes a real disclosure/oracle vector.
  The one privileged endpoint, `api-source-health`, queries as service-role over
  the anon-revoked `source_health` view and returns a **generic** error body
  (never the raw upstream PostgREST error), so its error path leaks no internals.

### Residual risks (accepted)

- A compromised *trusted* publisher can serve malicious-but-well-formed content;
  we cap, sanitize, and SSRF-guard it but cannot vouch for editorial integrity.
- Edge Functions run on Supabase's platform; we inherit its tenant isolation.
- LLM review bots are **not** hardened against prompt injection, hence the
  fork-gate; a malicious *trusted-author* PR is out of scope (single maintainer).

### Out of scope

- Memory-corruption / fuzzing harnesses — the worker is Go (memory-safe, race
  detector on in CI), the Edge Functions are Deno/TypeScript. There are no
  C/C++ targets, so ASAN/Firecracker-style sandboxes don't apply.
- End-user authentication, sessions, or PII handling — the product has none by
  design; introducing any requires updating the conformance docs first.

### How this document stays true

Update this file in the same PR when you: add an entry point or dependency;
add/alter a `SECURITY DEFINER` function, RLS policy, or GRANT; change the SSRF
transport or a parser limit; or add a workflow that handles secrets or fork
input. The `security-review` action appends this file to its audit prompt, so
keeping it current directly sharpens automated review.
