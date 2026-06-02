# Security Policy

## Supported Versions

Pulse Backend is a rolling, self-hosted deployment. There are no tagged
releases or long-lived release branches — production tracks the tip of
`master`. Only the latest `master` is supported and patched. Fixes are not
backported to older commits; pull the latest `master` to receive them.

| Version          | Supported          |
| ---------------- | ------------------ |
| `master` (latest)| :white_check_mark: |
| Any older commit | :x:                |

## Reporting a Vulnerability

**Please do not open a public GitHub issue for security vulnerabilities.**
Public issues disclose the problem to everyone before a fix is available.

Report privately through GitHub's built-in private vulnerability reporting:

1. Go to the repository's **Security** tab.
2. Click **Report a vulnerability** (under "Security advisories").
3. This opens a private security advisory visible only to you and the
   maintainer.

You can also reach the reporting form directly:
<https://github.com/BrunoCerberus/pulse-backend/security/advisories/new>

Please include, as best you can:

- A description of the issue and its potential impact.
- Steps to reproduce, or a proof of concept.
- The affected component (RSS worker, Edge Function, database migration, or
  CI workflow) and the relevant file paths or commit.

### Response expectations

This is a solo-maintained, open-source project worked on in spare time.
Best-effort targets:

- **Acknowledgement:** within a few days of the report.
- **Assessment and triage:** shortly after acknowledgement, with an
  indication of severity and next steps.
- **Fix:** prioritised by severity and merged to `master`. You will be
  credited in the advisory if you wish.

Please allow a reasonable window for a fix to ship before any public
disclosure, and coordinate timing through the private advisory.

## Scope

This backend aggregates **public RSS news only**. It does not collect, store,
or process any end-user personal data — no accounts, no sessions, no IP
logging, no analytics. See [`docs/privacy.md`](docs/privacy.md) for the full
privacy position.

The `author` byline stored on articles is a public-record journalist
attribution, processed under the journalism exemption (GDPR Art. 85 /
LGPD Art. 4 § II / CCPA §1798.145(k)). It is not considered end-user PII.

Because there is no end-user data, the most relevant vulnerability classes are
server-side request forgery (SSRF) via hostile feeds, injection, denial of
service against the worker or Edge Functions, secret exposure, and
supply-chain issues in dependencies.

## Triage and severity

The bottleneck in securing a codebase is not finding candidate issues — the
deterministic scanners and AI review (see below) surface plenty — it is
*verifying* and *triaging* them. This rubric keeps that judgment consistent.
See [`THREAT_MODEL.md`](THREAT_MODEL.md) for what we actually care about and
[`PATCHING.md`](PATCHING.md) for what happens after a finding is confirmed.

**Write the reasoning before assigning a severity.** For each finding, answer
these six questions explicitly first — assigning a label before reasoning anchors
you to a gut number and the model (or human) tends to inflate it:

1. **Reachability** — can untrusted input actually reach this code path, or is it
   behind the `service_role` GRANT boundary / an unreachable branch?
2. **Attacker control** — how much of the input does the attacker control (a full
   feed body vs. a single clamped integer)?
3. **Preconditions** — what must already be true to exploit it (a compromised
   publisher, a leaked key, an existing privilege)?
4. **Authentication** — does it require a trusted role? The public Edge API and
   `anon` reads are untrusted; `service_role` is trusted.
5. **Read vs. write** — does it disclose data, or corrupt/escalate?
6. **Blast radius** — one article, one source, the whole DB, or the worker host?

Then assign a level:

| Severity | Shape |
|----------|-------|
| **Critical** | Untrusted-reachable, attacker-controlled, leads to write/RCE or service-role-key disclosure with DB-wide blast radius. |
| **High** | Untrusted-reachable data exposure beyond the public projection, or a write/escalation gated only by a single control. |
| **Medium** | Real but gated — requires a trusted role, a compromised publisher, or has a capped blast radius (e.g. a DoS already bounded by a limit). |
| **Low** | Defence-in-depth gap with no current exploit path; fix when convenient. |

**Deduplicating findings.** Two reports are the *same* finding when they share a
file + vulnerability class and their line numbers are within ~10 of each other,
or when they describe the *same root cause* in different words / at multiple call
sites. Fix the root cause once and note the call sites (see `PATCHING.md`), rather
than filing one issue per site.

**Worked example (May-2026 hardening audit, migration 027).** A `SECURITY DEFINER`
write function did not pin `search_path`, so a caller able to create objects on
an earlier schema in the path could shadow a built-in and run code as the function
owner. Reasoning: *Reachability* — low, EXECUTE is granted only to `service_role`
(a trusted role); *Attacker control* — would need object-creation rights they don't
have; *Preconditions* — an already-privileged role; *Authentication* — trusted-role
only; *Read vs write* — write/escalation; *Blast radius* — DB-wide if reached.
Inherent shape is High (write + DB-wide), but the GRANT gate makes it untrusted-
unreachable, so it triaged as **Medium** and was fixed defensively (`search_path = ''`
plus the JWT-claim caller gate, asserted by `supabase/tests/security_invariants.sql`).

## Existing safeguards

Security is enforced continuously in CI, not just at review time:

- **Static analysis & scanning:** CodeQL, `gosec`, `govulncheck`, `gitleaks`,
  TruffleHog, and Trivy (filesystem vuln / secret / misconfiguration) run on
  every push and pull request, plus a weekly scheduled run. See
  [`.github/workflows/security.yml`](.github/workflows/security.yml) and
  [`.github/workflows/codeql.yml`](.github/workflows/codeql.yml).
- **SSRF-aware HTTP transport:** all outbound fetches for user-supplied
  content (RSS feeds, og:image extraction, full-content extraction) dial
  through an SSRF-aware transport that resolves the host once and rejects
  loopback, private (RFC 1918), link-local, multicast, and unspecified IP
  ranges, re-validating across redirects
  (`rss-worker/internal/httputil/`).
- **Least privilege in the database:** row-level security on every table,
  column-level grants exposing only the safe subset of `articles` to the anon
  role, and `SECURITY DEFINER` functions pinned with `search_path = ''`.
- **Dependency hygiene:** Dependabot updates and a CycloneDX SBOM artifact.
- **AI-assisted review:** every trusted-author PR gets a general Claude Code
  review (`claude-code-review.yml`) and a diff-scoped security review
  (`security-review.yml`, advisory) whose audit prompt is anchored to
  [`THREAT_MODEL.md`](THREAT_MODEL.md).
- **Committed threat model:** [`THREAT_MODEL.md`](THREAT_MODEL.md) names the
  entry points, assets, and trust boundaries and is kept current with the code.
- **100% Go test coverage gate** on every pull request.
