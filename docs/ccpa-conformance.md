---
last_reviewed: 2026-05-14
---

# CCPA / CPRA Conformance — Pulse Backend

This document records the Pulse Backend's position with respect to the
California Consumer Privacy Act of 2018 (CCPA — Civil Code §§ 1798.100
et seq.), as amended by the California Privacy Rights Act of 2020 (CPRA),
and the operational guard rails that keep that position true over time.

Companion documents:
- [`docs/privacy.md`](./privacy.md) — overall privacy posture.
- [`docs/lgpd-conformance.md`](./lgpd-conformance.md) — equivalent for the
  Brazilian Lei Geral de Proteção de Dados.
- [`docs/gdpr-conformance.md`](./gdpr-conformance.md) — equivalent for the
  European General Data Protection Regulation.
- [`docs/ropa.md`](./ropa.md) — Record of Processing Activities.
- [`docs/data-retention.md`](./data-retention.md) — retention policy.

## Scope

The Pulse Backend consists of:

- The Go RSS worker (`rss-worker/`) running on GitHub Actions.
- The Supabase Edge Functions (`supabase/functions/`).
- The Supabase PostgreSQL database (`supabase/migrations/`).

It does **not** cover the Pulse iOS client.

## Applicability assessment

CCPA / CPRA applies to a "business" that meets the §1798.140(d)
thresholds: gross annual revenue over $25M; or buys, sells, shares, or
receives personal information of 100,000+ California consumers or
households; or derives 50%+ of annual revenue from selling or sharing
personal information.

This project is a solo-maintained open-source backend with no commercial
activity, no revenue, and no end-user data. **None of the three
threshold criteria are met.** CCPA therefore does not impose obligations
on this system as a matter of law. This document is maintained as a
position statement to make that posture explicit for anyone reviewing
the project from a California consumer-privacy perspective.

Independently of the threshold question: CCPA / CPRA regulates
"personal information" as defined in §1798.140(v) — information that
identifies, relates to, describes, is reasonably capable of being
associated with, or could reasonably be linked, directly or indirectly,
with a particular California consumer or household. **This backend does
not process personal information so defined.** It aggregates public
news articles, stores their text and metadata, and serves them via a
read-only API. There are no user accounts, no authentication of end
users, no analytics, no behavioural tracking, no IP-address logging.

The `author` field on indexed articles captures journalist bylines.
CCPA §1798.145(k) preserves the right of free expression and recognises
journalistic activity; the indexing of public bylines for navigation
purposes is treated under that framing rather than as processing of a
California consumer's personal information.

Because no personal information is processed:

- **§1798.105 right to delete** does not trigger an obligation. There
  is nothing to delete.
- **§1798.106 right to correct** is not applicable.
- **§1798.110 right to know what personal information is collected**
  has the disposition "none collected".
- **§1798.115 right to know what is sold or shared** — none collected,
  therefore none sold, therefore §1798.115 has nothing to disclose.
- **§1798.120 right to opt out of sale or sharing** is not applicable
  because no sale or sharing occurs. The §1798.135 requirement to
  display a "Do Not Sell or Share My Personal Information" link
  therefore does not attach.
- **§1798.121 right to limit use of sensitive personal information**
  is not applicable; no sensitive personal information is collected.
- **§1798.125 non-discrimination** is moot because no consumer-facing
  transactions occur.

If a California consumer contacts the maintainer claiming their
personal information appears here — most plausibly as an `author`
byline — the response is to:

1. Confirm via search of the article corpus.
2. Remove the matching article(s).
3. Reply with confirmation. The article will re-appear in the index
   only if the source publisher re-syndicates it; the source can be
   deactivated to prevent recurrence.

This is a manual process, not an automated DSAR pipeline — the
appropriate posture for a system with no personal-information
processing.

## Operational guard rails

The substantive guard rails are enforced by the existing LGPD and GDPR
conformance workflows (`.github/workflows/lgpd-conformance.yml` and
`.github/workflows/gdpr-conformance.yml`). Both run on every PR and a
weekly cron, and both have been extended to catch CCPA-relevant
identifier patterns:

- **No US Social Security Number patterns** (`XXX-XX-XXXX`). Defended
  via ripgrep regex pass + `gitleaks` custom rules over full git
  history. SSN is the only US-specific personal identifier with a
  distinctive enough format to warrant a dedicated regex; other
  US-style identifiers (driver's license numbers, state ID numbers)
  vary too widely by jurisdiction for a regex ban to be useful.
- All the existing controls also apply: no email outside the
  allowlist, no IP-handling code in `rss-worker/`, no new tables
  outside `{categories, sources, articles, fetch_logs}`, no
  PII-implying column names, retention enforcement, RLS protection,
  the no-PII-redaction invariant.

**No PII redaction layer required** because no PII enters the system in
the first place. This invariant is asserted in `lgpd-conformance.md`
and `gdpr-conformance.md` and gated by the conformance workflows.

## Notice at Collection (§1798.130)

Not applicable. The §1798.130 requirement to provide a notice at or
before collecting personal information presupposes that personal
information is being collected. This backend does not collect personal
information from consumers.

## "Sale" and "sharing" (§1798.140(ad), (ah))

Not applicable. The backend does not sell or share personal information.
Article content is served to the Pulse iOS app via an authenticated
Supabase anon key; the content served is publicly-available RSS-syndicated
news, not consumer personal information.

## Consumer rights contact

`bruno.guitarpro@gmail.com`

This is the single contact point for any inquiry from a California
consumer or from the California Privacy Protection Agency (CPPA). The
maintainer will respond on a best-effort basis; this is an open-source
side project, not an enterprise service.

## Subprocessors / "service providers"

Within CCPA's framing, GitHub and Supabase would be "service
providers" if any personal information were involved. Because no
personal information is processed, neither receives California
consumer personal information through this system. The full
subprocessor list is maintained in [`docs/ropa.md`](./ropa.md).

## Audit log

| Date       | Change                                                |
|------------|-------------------------------------------------------|
| 2026-05-14 | Initial CCPA / CPRA position drafted; SSN regex added to the existing LGPD and GDPR pii-scan jobs. |
