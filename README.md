# Pulse Backend

Self-hosted news aggregation backend for the Pulse iOS app. Uses **Go** for RSS
fetching and **Supabase** (PostgreSQL + auto-generated REST API + Edge Functions)
for storage and delivery. Runs on free tiers — **$0/month**.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          GitHub Actions (Free)                               │
│                      Scheduled: Every 2 hours                                │
│                                                                              │
│    ┌─────────────────────────────────────────────────────────────────────┐  │
│    │                         Go RSS Worker                                │  │
│    │                                                                      │  │
│    │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐            │  │
│    │  │ Guardian │  │   BBC    │  │ Podcasts │  │ YouTube  │  ...      │  │
│    │  │   RSS    │  │   RSS    │  │   RSS    │  │   RSS    │            │  │
│    │  └────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘            │  │
│    │       └─────────────┴──────┬──────┴─────────────┘                   │  │
│    │                            ▼                                        │  │
│    │                   Parse → Deduplicate → Insert                      │  │
│    └────────────────────────────┼────────────────────────────────────────┘  │
└─────────────────────────────────┼───────────────────────────────────────────┘
                                  │
                                  ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Supabase (Free Tier)                                 │
│                                                                              │
│   ┌─────────────┐    ┌─────────────┐    ┌─────────────────────────────┐    │
│   │  articles   │    │   sources   │    │       Edge Functions        │    │
│   │  (7 days)   │    │ (136 feeds) │    │    (Caching Proxy Layer)    │    │
│   └─────────────┘    └─────────────┘    └─────────────────────────────┘    │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
                                  │
                                  ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                            Pulse iOS App                                     │
│                                                                              │
│   HTTP calls to Edge Functions with Cache-Control support                   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

The Go worker fetches 136 pre-configured feeds, parses them with `gofeed`,
enriches articles (og:image + full content), deduplicates on a SHA-256
`url_hash`, and batch-inserts into Supabase. Edge Functions sit in front as a
cache-and-guard proxy that the iOS app reads.

## Documentation

| Doc | What's inside |
|-----|---------------|
| **[Setup Guide](docs/setup.md)** | First-time deploy: Supabase project, migrations, GitHub secrets, Edge Functions |
| **[Content Sources](docs/content-sources.md)** | The 136 pre-configured feeds (EN / PT / ES articles, podcasts, video) |
| **[Development](docs/development.md)** | Local dev, environment variables, `make` commands, testing, repository layout |
| **[API Reference](docs/api-reference.md)** | Edge Function endpoints, parameters, and request guards |
| **[Database Schema](docs/database-schema.md)** | Tables, views, RPCs, indexes, and RLS |
| **[iOS Integration](docs/ios-integration.md)** | Wiring the Pulse iOS app to this backend |
| **[CI/CD & Security](docs/ci-cd.md)** | All workflows, branch protection, and the security pipeline |
| **[Operations Runbook](docs/operations-runbook.md)** | Monitoring, troubleshooting, and on-call procedures |
| **[Data Protection](docs/privacy.md)** | Privacy posture + [LGPD](docs/lgpd-conformance.md) / [GDPR](docs/gdpr-conformance.md) / [CCPA](docs/ccpa-conformance.md) / [ROPA](docs/ropa.md) / [retention](docs/data-retention.md) |

## Quick Start

1. Create a Supabase project and run the migrations in `supabase/migrations/`.
2. Add your GitHub secrets (`SUPABASE_URL`, `SUPABASE_SERVICE_ROLE_KEY`, …).
3. Enable the GitHub Actions workflows.
4. Deploy the Edge Functions (`supabase functions deploy`).

→ Full step-by-step walkthrough in **[docs/setup.md](docs/setup.md)**.

## Free Tier Limits

| Service | Limit | Our Usage | Status |
|---------|-------|-----------|--------|
| Supabase DB | 500 MB | ~90 MB | ✅ |
| Supabase API | 500K req/mo | ~200K | ✅ |
| Supabase Edge Functions | 500K invocations/mo | Varies | ✅ |
| GitHub Actions | 2,000 min/mo | ~720 min | ✅ |

## API at a Glance

```
Base URL: https://<project>.supabase.co/functions/v1

GET /api-categories       # 24h cache
GET /api-sources          # 1h cache
GET /api-articles         # 15min + ETag (304 support)
GET /api-search?q=…       # 1min private cache
GET /api-health           # no-store liveness probe
GET /api-source-health    # 60s — per-source health + DB size (powers the watchdog)
```

No authentication required — endpoints are public read-only. Full parameter
reference, response shapes, and request guards: **[docs/api-reference.md](docs/api-reference.md)**.

## Repository Layout

```
pulse-backend/
├── rss-worker/     # Go RSS fetcher + enrichment (internal/ packages)
├── supabase/       # migrations, Edge Functions, SQL invariant tests
├── docs/           # all documentation (see index above)
└── .github/        # 15 workflows + governance
```

→ Full annotated layout in **[docs/development.md](docs/development.md#repository-layout)**.

## Data Protection

The backend asserts and enforces a **no-end-user-PII** posture: it aggregates
public RSS news only and processes no personal data of identified or identifiable
natural persons. Two conformance workflows (LGPD and GDPR/CCPA) act as living
guard rails — PRs that would erode the posture fail CI. See
[docs/privacy.md](docs/privacy.md) and the per-regulator docs linked above.

## Cost & Scaling

**Monthly cost: $0** (within free tiers). GitHub Actions is free for public
repositories; the 2,000-min/mo allowance applies to private repos.

If you outgrow the free tier:

| Upgrade | Cost | Benefit |
|---------|------|---------|
| Supabase Pro | $25/mo | 8 GB DB, 250 GB bandwidth |
| GitHub Actions | $4/1000 min | More workflow minutes |

For day-2 scaling levers (retention tuning, cache durations, deactivating
sources), see the [Operations Runbook](docs/operations-runbook.md).

## License

MIT
