---
last_reviewed: 2026-05-14
---

# Record of Processing Activities — Pulse Backend

Maintained to satisfy the documentation expectations of GDPR Art. 30
and the analogous documentation expectation under LGPD Art. 37, even
though neither obligation strictly applies because the system does not
process personal data.

Companion documents:
- [`docs/privacy.md`](./privacy.md)
- [`docs/lgpd-conformance.md`](./lgpd-conformance.md)
- [`docs/gdpr-conformance.md`](./gdpr-conformance.md)
- [`docs/data-retention.md`](./data-retention.md)

## Controller

| Field           | Value                                  |
|-----------------|----------------------------------------|
| Name            | Bruno Mello (project maintainer)       |
| Role            | Sole maintainer of the open-source repo |
| Contact         | bruno.guitarpro@gmail.com              |
| Representative  | Not applicable (no EU representative — see `docs/gdpr-conformance.md`) |

## Data Protection Officer

Not appointed. Appointment thresholds under GDPR Art. 37(1) and LGPD
Art. 41 are not met because no personal data is processed.

## Processing activities

A single processing activity is documented for completeness, even though
it does not constitute "processing of personal data" under either
regulation.

### Activity 1 — Public RSS aggregation

| Field                            | Value |
|----------------------------------|-------|
| Purpose                          | Aggregate publicly-syndicated news RSS feeds, normalise them, and serve them via a read-only API to the Pulse iOS app. |
| Categories of data subjects      | None. The system does not process personal data of identifiable natural persons. |
| Categories of personal data      | None. The `author` field captures public journalistic bylines treated under the journalism exemption (GDPR Art. 85 / LGPD Art. 4 § II). |
| Recipients                       | Read-only public API; the only consumer is the Pulse iOS client, authenticated via the Supabase anon key. |
| Cross-border transfers           | None applicable. Supabase project region and GitHub Actions runner region are configuration details, not transfers of personal data. |
| Retention period                 | Articles: 7 days. `fetch_logs`: 7 days. No backups beyond Supabase platform-managed PITR. |
| Lawful basis (GDPR Art. 6)       | Not applicable — no personal data is processed. |
| Legal basis (LGPD Art. 7)        | Not applicable — no personal data is processed. |
| Technical / organizational measures | TLS, RLS, SSRF-aware HTTP client, per-host rate limiting, 100% Go test coverage, weekly security audit (`security.yml`), conformance workflows (`lgpd-conformance.yml`, `gdpr-conformance.yml`). |

## Subprocessors

The conformance workflow asserts that every row below remains present in
this table. Update both the table and the workflow allowlist if a
subprocessor is added or removed.

| Subprocessor | Purpose                              | Region            | DPA / contract                                                     |
|--------------|--------------------------------------|-------------------|--------------------------------------------------------------------|
| GitHub       | Source hosting, CI/CD, scheduled cron, Actions runner logs, repository secrets. | US (Actions infra). | GitHub Customer Data Processing Addendum covers Actions runners and Secrets storage. |
| Supabase     | Managed PostgreSQL, Edge Functions, project secrets. | Project-bound (selected at project creation). | Supabase Data Processing Agreement covers managed services. |

## Data flow summary

```
Public RSS publishers --(HTTPS, outbound only)--> GitHub Actions runner
   |                                                   |
   |                                                   v
   |                                              Go RSS worker
   |                                                   |
   |                                                   v
   `--(HTTPS)----------------------------------> Supabase PostgreSQL
                                                       |
                                                       v
                                                  Edge Functions
                                                       |
                                                       v
                                                  Pulse iOS app (anon key)
```

No data enters from end users. The arrow into Supabase carries public
article content and operational telemetry; the arrow into Edge Functions
carries cached API responses; the arrow into the iOS app carries
read-only article data.

## Audit log

| Date       | Change                                                |
|------------|-------------------------------------------------------|
| 2026-05-14 | Initial ROPA drafted alongside the conformance workflows. |
