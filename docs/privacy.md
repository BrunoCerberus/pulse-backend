---
last_reviewed: 2026-05-14
---

# Pulse Backend — Privacy Position

Pulse Backend is the server-side component of the Pulse iOS news reader. It
aggregates publicly-available RSS feeds, normalises them, and serves them to
the iOS app via a caching API. **It does not collect, store, or process any
end-user personal data.** This document describes what the system handles and
why standard data-protection obligations (DSAR pipelines, DPO appointment,
consent records, etc.) do not apply.

This is a maintainer-led, open-source project. If you reach out about your
data and it is hosted here, the maintainer will respond personally — see the
**Contact** section.

## What this backend stores

Three buckets, all derived from public sources or operational telemetry:

1. **Articles** — title, summary, content, URL, optional `image_url`,
   `thumbnail_url`, `author` (journalist byline), `published_at`,
   `media_url` / `media_duration` for podcasts and videos, and the language
   inherited from the source. Source: public RSS feeds enumerated in
   `supabase/migrations/`.
2. **Sources and categories** — static configuration: feed URL, display name,
   slug, language, fetch interval, circuit-breaker state. No subscriber
   information.
3. **Operational logs (`fetch_logs`)** — per-fetch summary: `run_id`,
   `sources_processed`, `articles_inserted`, `errors`, status. No user
   identifiers, no IP addresses, no request fingerprints.

The `author` field captures journalist bylines (e.g. "Jane Smith, Reuters").
These are public-record professional attributions and are processed under the
journalism exemption recognised by both LGPD (Art. 4 § II) and GDPR
(Art. 85 + Recital 153). See `docs/lgpd-conformance.md` and
`docs/gdpr-conformance.md` for the regulator-specific reasoning.

## What this backend does NOT collect

- No user accounts, sessions, or authentication of end users.
- No analytics, tracking pixels, cookies, or fingerprinting.
- No IP-address logging. The Go RSS worker only initiates outbound
  connections; it does not serve user requests. Supabase Edge Functions
  serve the iOS app but do not log client IPs in application code.
- No reading history, bookmarks, or preference data. The iOS client may
  store such state locally; the backend never sees it.
- No third-party analytics SDKs, advertising identifiers, or behavioural
  profiling.

## Subprocessors

The backend uses two managed services. Both are bound by their own
data-processing agreements; neither receives end-user personal data from
this system.

- **GitHub** — Source hosting, CI/CD (scheduled RSS fetch, deploy
  pipelines, security scans), Actions runner logs. Covered by GitHub's
  standard Customer Data Processing Addendum.
- **Supabase** — Managed PostgreSQL, Edge Functions runtime, project
  secrets. Region selected at project creation. Covered by Supabase's
  Data Processing Agreement.

A full list with purpose, region, and DPA status is maintained in
[`docs/ropa.md`](./ropa.md).

## Encryption

- **In transit**: all Supabase API traffic uses TLS. RSS feed fetches use
  HTTPS where supported by the source; new sources added via migrations
  must use `https://` (enforced by the operational-controls check in
  `.github/workflows/lgpd-conformance.yml` and `gdpr-conformance.yml`).
- **At rest**: Supabase-managed PostgreSQL encryption at rest (provider
  configuration). No application-level encryption on top — the data is
  public news content, so encryption is a defense-in-depth measure, not
  a confidentiality requirement.

## Retention

- Articles: 7 days. Auto-purged by daily cleanup workflow.
- `fetch_logs`: 7 days (same cleanup pass).
- Backups: Supabase platform-managed PITR only. No long-term archives.

Details and rationale in [`docs/data-retention.md`](./data-retention.md).

## Security controls

- Static analysis: `gitleaks`, `TruffleHog`, `gosec`, `govulncheck`, `Trivy`
  (see `.github/workflows/security.yml`).
- SSRF-aware HTTP client for all outbound RSS / og:image / content fetches
  (`rss-worker/internal/httputil/`).
- Row-level security enabled on every table. Column-level grants on
  `articles` expose only the safe subset to the anon role.
- 100% Go test coverage gate on every PR.
- Per-host rate limiting on outbound HTTP to be a good citizen of source
  publishers' infrastructure.

## Contact

Maintainer: `bruno.guitarpro@gmail.com`

If you are a data subject who believes personal information about you
appears in this system — most likely as an `author` byline on an indexed
article — email the maintainer with the article URL or your name. Removal
is a manual operation and is processed without ceremony.

## Triggers for the next review

This document should be revisited if any of the following land:

- An accounts / authentication system is added.
- Reading history, bookmarks, or any user-specific state is stored
  server-side.
- A new third-party subprocessor is added (analytics, telemetry,
  feature flags, email).
- A new database table is added that contains user-identifying columns.
- A jurisdiction-specific request (LGPD ANPD, GDPR supervisory authority)
  is received.

The structural-integrity job in the conformance workflows will catch new
tables and suspicious column names automatically — but the maintainer is
still expected to update this document in the same PR.

## Changes to this policy

Versioned in git. Significant changes ship via PR with the `docs:` prefix
and are reviewed alongside the code change that prompted them.
