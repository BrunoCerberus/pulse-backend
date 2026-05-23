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
- **100% Go test coverage gate** on every pull request.
