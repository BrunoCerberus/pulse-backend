## Summary

<!-- One or two bullets: what changed and why. -->

## Test plan

<!-- How this was verified. Tick what applies; delete the rest. -->

- [ ] `make test-go-race` passed
- [ ] `make test-deno` passed (Edge Functions changed)
- [ ] Smoke-tested locally (`make run` / `make backfill-*`)
- [ ] Migration queued (`supabase db push --dry-run` shows expected pending list)

## Notes

<!--
Migrations to apply, env vars added, rollback steps, anything reviewers should
know before merging. Delete this section if none.
-->
