---
last_reviewed: 2026-05-14
---

# GDPR Conformance — Pulse Backend

This document records the Pulse Backend's position with respect to the
General Data Protection Regulation (Regulation (EU) 2016/679 — GDPR) and
the operational guard rails that keep that position true over time.

Companion documents:
- [`docs/privacy.md`](./privacy.md) — overall privacy posture.
- [`docs/lgpd-conformance.md`](./lgpd-conformance.md) — equivalent for the
  Brazilian Lei Geral de Proteção de Dados.
- [`docs/ccpa-conformance.md`](./ccpa-conformance.md) — equivalent for the
  California Consumer Privacy Act / California Privacy Rights Act.
- [`docs/ropa.md`](./ropa.md) — Record of Processing Activities (Art. 30).
- [`docs/data-retention.md`](./data-retention.md) — retention policy.

## Scope

The Pulse Backend consists of:

- The Go RSS worker (`rss-worker/`) running on GitHub Actions.
- The Supabase Edge Functions (`supabase/functions/`).
- The Supabase PostgreSQL database (`supabase/migrations/`).

This document does **not** cover the Pulse iOS client.

## Applicability assessment

GDPR Art. 2 + 3 set the material and territorial scope. The regulation
applies to "the processing of personal data wholly or partly by automated
means", where personal data is "any information relating to an
identified or identifiable natural person".

This backend processes **no personal data of identified or identifiable
natural persons**. It aggregates public news articles, stores text and
metadata, and serves them via a read-only API. No user accounts, no
authentication of end users, no analytics, no IP logging, no profiling.

The `author` field on indexed articles captures journalist bylines.
GDPR Art. 85 and Recital 153 recognise the freedom of expression and
information, including journalistic activity, as a category that Member
States must reconcile with the right to data protection. The
intermediary indexing of public bylines for navigation purposes — i.e.
treating the journalist's name as a public-record professional
attribution — falls within this framing rather than within a
personal-data processing operation requiring a lawful basis under Art. 6.

Because no personal data is processed:

- The lawful-basis catalogue (Art. 6) is **not applicable**. No basis is
  established because no personal data is processed.
- Data-subject rights under Art. 15–22 (access, rectification, erasure,
  restriction, portability, objection, automated decision-making) do
  **not** trigger an obligation. There is nothing to disclose, port, or
  rectify.
- A Data Protection Officer (Art. 37(1)) is **not appointed**. None of
  the appointment thresholds are met: no public authority, no large-scale
  systematic monitoring of data subjects, no large-scale processing of
  special-category data.
- An EU representative (Art. 27) is **not appointed**. The backend does
  not offer goods or services to EU data subjects and does not monitor
  their behaviour in a manner that would trigger Art. 3(2). It serves a
  read-only news feed to a client app whose users may or may not be in
  the EU; no targeting and no monitoring occurs at the backend layer.
- A Data Protection Impact Assessment (Art. 35) is **not required**. No
  high-risk processing operation exists to assess.

If an EU data subject contacts the maintainer claiming personal
information about them appears here — most plausibly as an `author`
byline — the response is to:

1. Confirm via search of the article corpus.
2. Remove the matching article(s).
3. Reply with confirmation. The article will re-appear in the index only
   if the source publisher re-syndicates it; the source can be
   deactivated to prevent recurrence.

This is a manual process, not an automated DSAR pipeline — the
appropriate posture for a system with no personal-data processing.

## Operational guard rails

The conformance workflow `.github/workflows/gdpr-conformance.yml`
enforces the following invariants on every PR and weekly cron:

- **No IBAN patterns** in source, tests, migrations, or docs. Defended
  via ripgrep regex pass + `gitleaks` custom rules over full git history.
- **No EU/EEA phone-number patterns** (`+CC` prefix restricted to the
  EU/EEA dialing-code set). Avoids the false-positive surface of
  arbitrary E.164 numbers.
- **No disallowed email addresses**. The maintainer email and a small set
  of RFC 6761 reserved-domain placeholders are listed in
  `.github/pii-allowlist.txt`. Any other email literal fails the build.
- **No IP-address-shaped variables flowing into the logger**. Heuristic
  regex pass over `rss-worker/` Go source. This is defense-in-depth; the
  actual guarantee is architectural — the Go worker does not serve user
  requests, so no client IP ever enters its address space.
- **No plaintext `http://` URLs** in `supabase/migrations/` outside a
  short technical allowlist (XML namespaces, W3C schema URLs).
- **Cleanup workflow present and correct** — `.github/workflows/cleanup.yml`
  must exist and invoke `./rss-worker cleanup`.
- **Retention literal matches code** — `ArticleRetentionDays = 7` in
  `rss-worker/internal/config/config.go`, and the literal `7 days` appears
  in `docs/data-retention.md`.
- **RLS not disabled** on `articles`, `sources`, `categories`, or
  `fetch_logs` in any migration.
- **No new database tables** outside the existing allowlist without an
  explicit conformance review.
- **No suspicious column names** in migrations.

**No PII redaction layer required** because no PII enters the system in
the first place. (This sentence is asserted verbatim by the
operational-controls job; do not paraphrase without updating the
workflow.)

## Supervisory-authority and data-subject contact

`bruno.guitarpro@gmail.com`

The maintainer will acknowledge any inquiry from an EU supervisory
authority or data subject within a reasonable window (best effort; this
is an open-source side project, not an enterprise service).

## Cross-border data residency

The Supabase project region is selected at project creation and may be
inside or outside the EEA. GDPR Chapter V cross-border transfer
provisions (Art. 44–49) are **not applicable** because no personal data
of EU data subjects is transferred — there is no personal data to
transfer.

## Audit log

| Date       | Change                                                |
|------------|-------------------------------------------------------|
| 2026-05-14 | Initial document drafted alongside conformance workflow. |
