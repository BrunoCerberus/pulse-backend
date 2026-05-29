# Operations Runbook

Guide for monitoring, troubleshooting, and maintaining the Pulse Backend.

## Monitoring

### Fetch Logs

Check recent fetch operations in Supabase Table Editor or via SQL:

```sql
-- Last 10 fetch runs
SELECT
    started_at,
    completed_at,
    status,
    sources_processed,
    articles_inserted,
    articles_skipped,
    errors
FROM fetch_logs
ORDER BY started_at DESC
LIMIT 10;
```

```sql
-- Failed fetches in last 24 hours
SELECT * FROM fetch_logs
WHERE status = 'failed'
  AND started_at > NOW() - INTERVAL '24 hours'
ORDER BY started_at DESC;
```

### GitHub Actions

View workflow runs:
1. Go to Repository → Actions tab
2. Check `fetch-rss.yml` for RSS fetching (every 2 hours)
3. Check `cleanup.yml` for article cleanup (daily at 3 AM UTC)
4. Check `watchdog.yml` for source-health alerts (every 6 hours, fails the
   job + sends GitHub email when `circuit_open` / `stale` /
   `high_failure` / `database.quota_pct` cross thresholds defined inline)
5. Check `deploy.yml` for production deploys — gated by the
   `production` GitHub Environment (required-reviewer approval,
   master-only); deploys pause for human approval in the Actions UI.
   See [ci-cd.md](ci-cd.md) for the full ordered pipeline.

### Key Metrics to Monitor

| Metric | Normal Range | Alert Threshold |
|--------|--------------|-----------------|
| Articles inserted per run | 5-50 | 0 for 3+ consecutive runs |
| Sources processed | 136 (all configured sources; live active count may be lower) | < 100 |
| Errors per run | 0-2 | > 5 |
| Fetch duration | 1-3 minutes | > 10 minutes |

---

## Common Issues

### Issue: No Articles Being Inserted

**Symptoms:** `articles_inserted = 0` for multiple consecutive runs

**Possible Causes:**
1. RSS feeds haven't updated (normal for quiet periods)
2. All articles are duplicates (normal)
3. Feed URLs changed or became invalid
4. Network issues with RSS sources

**Resolution:**
1. Check `errors` column in fetch_logs for specific failures
2. Manually test feed URLs: `curl -I <feed_url>`
3. Run manual fetch to see detailed logs:
   ```bash
   cd rss-worker && go run .
   ```

---

### Issue: GitHub Action Failing

**Symptoms:** Red X on workflow runs

**Resolution:**
1. Click on failed run to see logs
2. Common causes:
   - Secret expired (regenerate `SUPABASE_SERVICE_ROLE_KEY`)
   - Go build failure (check go.mod dependencies)
   - Timeout (increase timeout in workflow YAML)

---

### Issue: Missing og:images

**Symptoms:** Articles have low-quality or missing images

**Resolution:**
Run image backfill manually:
```bash
cd rss-worker
go run . backfill-images
```

This fetches og:image from article pages for up to 500 articles per run.

---

### Issue: Missing Article Content

**Symptoms:** `content` field is NULL for many articles

**Resolution:**
Run content backfill manually:
```bash
cd rss-worker
go run . backfill-content
```

This extracts full text using go-readability for up to 200 articles per run.

---

### Issue: Duplicate Articles

**Symptoms:** The same article appears more than once in the feed

**Possible Causes:**
The system deduplicates on a SHA-256 `url_hash` with a `UNIQUE` constraint. A
source that publishes the *same* article under *different* URLs (e.g. tracking
parameters, or `/amp` variants) defeats hash-based dedup.

**Resolution:**
1. Check whether the source emits varying URLs for one story
2. The `url_hash` column enforces uniqueness, so identical URLs are already
   collapsed at insert time — only genuinely distinct URLs slip through
3. If a source is a repeat offender, consider deactivating it (see
   [Deactivating a Source](#deactivating-a-source))

---

### Issue: Database Growing Too Large

**Symptoms:** Approaching Supabase row limits

**Resolution:**
1. Reduce retention period temporarily:
   ```sql
   SELECT cleanup_old_articles(14);  -- Keep only 14 days
   ```
2. Run cleanup manually:
   ```bash
   cd rss-worker && go run . cleanup
   ```
3. Check for sources producing excessive articles and consider deactivating

---

## Manual Operations

### Adding a New RSS Source

1. Insert via Supabase Table Editor or SQL:
   ```sql
   INSERT INTO sources (name, slug, feed_url, website_url, category_id, language, is_active)
   VALUES (
       'Source Name',
       'source-slug',
       'https://example.com/feed.xml',
       'https://example.com',
       (SELECT id FROM categories WHERE slug = 'technology'),
       'en',
       true
   );
   ```

2. Trigger manual fetch to verify:
   - GitHub Actions → fetch-rss → Run workflow

### Deactivating a Source

```sql
UPDATE sources SET is_active = false WHERE slug = 'source-slug';
```

### Running Manual Cleanup

```bash
# Via GitHub Actions
# Go to Actions → cleanup.yml → Run workflow

# Or locally
cd rss-worker
export SUPABASE_URL="https://xxx.supabase.co"
export SUPABASE_SERVICE_ROLE_KEY="your-key"
go run . cleanup
```

### Clearing Fetch Logs

```sql
-- Keep only last 7 days of logs
DELETE FROM fetch_logs WHERE started_at < NOW() - INTERVAL '7 days';
```

> **Note:** As of the latest update, fetch log cleanup is automated as part of the daily cleanup job.

---

## Emergency Procedures

### Rollback Bad Data

If corrupted articles were inserted:

```sql
-- Find articles from specific time range
SELECT id, title, created_at
FROM articles
WHERE created_at BETWEEN '2024-01-15 10:00:00' AND '2024-01-15 11:00:00';

-- Delete if confirmed
DELETE FROM articles
WHERE created_at BETWEEN '2024-01-15 10:00:00' AND '2024-01-15 11:00:00';
```

### Disable All Fetching

1. Disable GitHub Actions workflow:
   - Repository → Actions → fetch-rss → ... → Disable workflow

2. Or set all sources inactive:
   ```sql
   UPDATE sources SET is_active = false;
   ```

### Regenerate Supabase Keys

The watchdog workflow no longer sends the service-role key as a bearer to
`api-source-health` (since migration 027 / the matching Edge Function
update). Rotating the key only requires updating writers + Edge Function
project secrets:

1. Supabase Dashboard → Settings → API → Regenerate Service Role key.
2. Update the `production` GitHub Environment secrets:
   - Settings → Environments → production
   - Update `SUPABASE_ACCESS_TOKEN` (for deploys) and `SUPABASE_SERVICE_ROLE_KEY`
     (used by `fetch-rss.yml`, `cleanup.yml`, `backfill.yml`).
3. The `api-source-health` Edge Function reads
   `SUPABASE_SERVICE_ROLE_KEY` from the Supabase project's auto-injected
   env, so no Edge Function redeploy is needed after a rotation in the
   Supabase dashboard.
4. The watchdog workflow only needs `SUPABASE_URL` — no key required.

Rotate proactively after merging the security-hardening PR for the first
time, since the previous key spent ≥ one cycle in the Authorization
header of watchdog requests and is potentially visible in Supabase
function logs.

---

## Scaling Considerations

### Approaching Free Tier Limits

Supabase free tier limits:
- 500 MB database size
- 2 GB bandwidth/month
- 50,000 monthly active users

**Mitigations:**
1. Reduce article retention: `cleanup_old_articles(14)`
2. Reduce fetch frequency (edit workflow cron)
3. Deactivate low-value sources
4. Upgrade to Pro tier ($25/month)

### High Traffic Scenarios

Edge Functions handle caching automatically:
- Articles: 15 min cache + 30 min stale-while-revalidate
- Categories/Sources: Long cache (1-24 hours)

For higher traffic:
1. Increase cache durations in `_shared/cache.ts`
2. Add CDN in front of Supabase (Cloudflare, etc.)
3. Consider read replicas (Supabase Pro feature)
