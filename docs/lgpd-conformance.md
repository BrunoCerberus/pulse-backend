---
last_reviewed: 2026-05-14
---

# LGPD Conformance — Pulse Backend

This document records the Pulse Backend's position with respect to the
Lei Geral de Proteção de Dados Pessoais (LGPD — Lei nº 13.709/2018) and
the operational guard rails that keep that position true over time.

Companion documents:
- [`docs/privacy.md`](./privacy.md) — overall privacy posture.
- [`docs/gdpr-conformance.md`](./gdpr-conformance.md) — equivalent for the
  European General Data Protection Regulation.
- [`docs/ropa.md`](./ropa.md) — Record of Processing Activities.
- [`docs/data-retention.md`](./data-retention.md) — retention policy.

## Scope

The Pulse Backend consists of:

- The Go RSS worker (`rss-worker/`) running on GitHub Actions.
- The Supabase Edge Functions (`supabase/functions/`).
- The Supabase PostgreSQL database (`supabase/migrations/`).

It does **not** cover the Pulse iOS client, which has its own App Privacy
disclosure in App Store Connect and is the responsibility of the
publishing entity for that app.

## Applicability assessment

LGPD (Art. 3) applies to processing operations involving personal data of
natural persons that occur in Brazilian territory, target Brazilian
markets, or process data collected in Brazil.

This backend processes **no personal data of natural persons**. It
aggregates public news articles via RSS, stores their text + metadata,
and serves them via a read-only API. There are no user accounts, no
authentication of end users, no analytics, no behavioural tracking, no
device identifiers, and no IP-address logging.

The `author` field on indexed articles captures journalist bylines.
LGPD Art. 4 § II expressly excludes processing performed exclusively for
journalistic and artistic purposes from the law's main provisions. The
backend's role here is indexing public bylines for navigation —
analogous to a library catalogue, not a personal-data registry.

Because no personal data is processed:

- LGPD Art. 18 data-subject rights (access, rectification, deletion,
  portability, anonymisation, revocation of consent) do **not** trigger
  an obligation. There is nothing to disclose, port, or anonymise.
- A formal DPO (Encarregado, Art. 41) is **not appointed**. The
  threshold criteria are not met because no personal data is processed.
  The maintainer (`bruno.guitarpro@gmail.com`) serves as the single
  contact point for any inquiry from a data subject, the ANPD, or any
  other party.
- A Data Protection Impact Assessment (Art. 38) is **not required**. The
  ANPD's discretion to request one in cases of high risk is acknowledged;
  the assessment would conclude "no personal data processed".

If a Brazilian data subject contacts the maintainer claiming personal
information about them appears here — most plausibly as an `author`
byline — the response is to:

1. Confirm via search of the article corpus.
2. Remove the matching article(s).
3. Reply with confirmation. The article will re-appear in the index only
   if the source publisher re-syndicates it; in that case the source can
   be deactivated.

This is a manual process, not an automated DSAR pipeline. That is the
appropriate posture for a system with no personal-data processing.

## Operational guard rails

The conformance workflow `.github/workflows/lgpd-conformance.yml`
enforces the following invariants on every PR and weekly cron:

- **No CPF or CNPJ patterns** in source, tests, migrations, or docs (CPF
  format `XXX.XXX.XXX-XX`; CNPJ format `XX.XXX.XXX/XXXX-XX`). Defended
  via ripgrep regex pass + `gitleaks` custom rules over full git history.
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
- **No new database tables** outside the existing allowlist
  (`articles`, `sources`, `categories`, `fetch_logs`) without an explicit
  conformance review.
- **No suspicious column names** in migrations (e.g. `email`, `phone`,
  `password`, `user_id`, `client_ip`, `session_id`, `cpf`, `cnpj`,
  `ssn`, `iban`).

**No PII redaction layer required** because no PII enters the system in
the first place. (This sentence is asserted verbatim by the
operational-controls job; do not paraphrase without updating the
workflow.)

## ANPD and data-subject contact

`bruno.guitarpro@gmail.com`

The maintainer will acknowledge any inquiry from the ANPD or a Brazilian
data subject within a reasonable window (best effort; this is an
open-source side project, not an enterprise service).

## Cross-border data residency

GitHub Actions runners and the Supabase project may be located outside
Brazil. LGPD Art. 33 cross-border transfer provisions are **not
applicable** because no personal data of Brazilian residents is
transferred — there is no personal data to transfer.

## Audit log

| Date       | Change                                                |
|------------|-------------------------------------------------------|
| 2026-05-14 | Initial document drafted alongside conformance workflow. |
