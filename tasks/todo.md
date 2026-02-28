# Reduce Supabase Resource Usage

## Completed Tasks

- [x] **Change 1:** Batch article inserts — POST arrays of 50 articles (~500→~10 calls/cycle)
- [x] **Change 2:** Batch image updates via RPC — single RPC call with all {url_hash, image_url} pairs
- [x] **Change 3:** Batch source last-fetched updates — single PATCH with PostgREST `in` filter
- [x] **Change 4:** Edge Function in-memory caching — categories (1h TTL), sources (30min TTL)
- [x] **Change 5:** Adaptive fetch frequency — podcasts/videos every 6h instead of 2h
- [x] **Change 6:** Denormalize source/category into articles — eliminate JOINs in view

## Verification

- All Go tests pass (7/7 packages)
- Race detector clean
- Coverage: main 80.4%, config 100%, database 81.3%, httputil 100%, logger 94.4%, models 100%, parser 92.7%
- `go vet ./...` clean

## Remaining Manual Steps

1. Apply migration 014 (batch_update_article_images RPC) via `supabase db push`
2. Apply migration 015 (fetch_interval_hours column) via `supabase db push`
3. Apply migration 016 (denormalize articles, recreate view) via `supabase db push`
4. Deploy Edge Functions (categories and sources updated with caching)
