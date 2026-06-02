# Patching Security Findings

How a confirmed finding becomes a merged fix. This codifies the *patch* step of
the find → verify → triage → patch loop. Pair it with
[`THREAT_MODEL.md`](THREAT_MODEL.md) (what we protect) and the triage rubric in
[`SECURITY.md`](SECURITY.md) (what severity it is).

The point of writing this down is repeatability: the project already patches this
way by habit (the 100% coverage gate, `supabase/tests/security_invariants.sql`,
migration 027's sweep across *all five* `SECURITY DEFINER` functions, migration
033 fixing the caller gate in the same five). This is that habit, made explicit.

## Principles

- **Fix the root cause, not the symptom or the single call site.**
- **Smallest change that fixes the root cause** — no refactoring, no drive-by
  cleanups in the same PR. They dilute review and widen the blast radius.
- **A human owns the patch.** AI tools (the `security-review` action, Claude
  Code) generate candidate fixes; the maintainer verifies and is accountable for
  what merges. Generated patches fail in predictable ways — fixing the symptom,
  or over-restricting to the point of rejecting legitimate feeds. Check both.
- **Don't break legitimate input.** A fix that rejects valid publisher content
  (a real RSS quirk, a valid redirect) trades a security bug for an availability
  bug. Verify against real feeds, not just the PoC.

## The validation ladder

Climb every rung. Don't merge until the top.

1. **Reproduce first.** Write a test that *fails on the current code* and
   demonstrates the issue:
   - Go → a table-driven test in the relevant `_internal` package
     (`httptest.Server` for fetch paths; recall `SetAllowLoopback` for SSRF
     tests behind the loopback guard).
   - Edge Functions → a Deno test (save/restore `globalThis.fetch` +
     `Deno.env`).
   - Database → a behavioral check or a new assertion in
     `supabase/tests/security_invariants.sql`.

   > Failure to produce a PoC is **not** proof of a false positive — if you
   > can't reproduce, budget more verification, don't assume it's safe.

2. **Fix the root cause.** Address the underlying flaw, not the specific input
   that triggered it.

3. **Hunt variants — two levels.**
   - *Same pattern:* the same mistake at other call sites (e.g. another fetch
     path missing the SSRF guard; another field missing a length cap).
   - *Same class:* other functions of the same shape (e.g. when one
     `SECURITY DEFINER` function needs `search_path = ''`, audit *all* of them —
     that is exactly what migration 027 did).

4. **Validate.**
   - Build + the new test now passes.
   - Full suite is green: `make test` (Go race detector + the **100% coverage
     gate**, Deno lint/fmt/tests).
   - For schema changes: `migrations-ci.yml` applies every migration from scratch
     and re-runs `security_invariants.sql`.
   - **Re-attack:** confirm the original PoC no longer reproduces.
   - **No regressions:** confirm valid feeds/requests still work.

5. **Re-review (fresh adversarial pass).** Let a fresh set of eyes try to break
   the patch — the `security-review.yml` action runs automatically on the PR, or
   do a manual fresh-context review. If the fix establishes a property that must
   never regress, **encode it as a new invariant** in `security_invariants.sql`
   (defence in depth, like the caller-gate assertions) so CI guards it forever.

## Ownership & disclosure

- Track non-sensitive findings with the **Security finding (tracking)** issue
  template; never put an exploitable, unfixed vulnerability in a public issue.
- For privately-reported vulnerabilities, coordinate the fix and timing through
  the **private GitHub advisory** (see `SECURITY.md`), and credit the reporter.
- All changes land via PR to `master` (squash-only, branch-protected — direct
  pushes are blocked). The fix and any new test/invariant go in the same PR;
  update `THREAT_MODEL.md` if the change adds or alters a control.
