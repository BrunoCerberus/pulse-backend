#!/usr/bin/env bash
#
# Shared endpoint contract suite for the Pulse backend.
#
# Called from TWO workflows so the two smoke tests can never drift:
#   - .github/workflows/migrations-ci.yml → "Edge Function contract tests"
#     job: runs against the local Supabase stack (Postgres + PostgREST + Kong
#     + Edge Runtime) with every migration applied — the functions are
#     otherwise only verified in production, so cross-layer drift fails the
#     PR instead of the release.
#   - .github/workflows/deploy.yml → post-deploy smoke test against the
#     production gateway: the only signal that a fix-forward release is good.
#
# Usage: check-endpoints.sh <base-url> [annotation-title] [results-file]
#   base-url       gateway prefix, e.g. https://<host>/functions/v1
#   title          the ::error annotation title (default: "Endpoints")
#   results-file   markdown results table (default: /tmp/check-endpoints-results.md)
#
# Every endpoint is a plain GET (all six functions are verify_jwt = false in
# supabase/config.toml) with no side effects, checked for:
#   - HTTP 200 or 206 — PostgREST answers Partial Content whenever `limit`
#     makes the response a subset of the matching rows, which is every
#     paged list;
#   - a body satisfying the jq SHAPE assertion — the DB may legitimately be
#     empty on a fresh project, so `type == "array"` is the right assertion
#     for list endpoints; a PostgREST error body is an object, not an array,
#     so a broken view or a revoked grant still fails the suite;
#   - the exact documented Cache-Control.
#
# stdout carries the per-endpoint log lines and the GitHub annotations; the
# results table goes to results-file (written on EVERY run, including a
# partial failure) so a caller's always() annotate step can render it on both
# paths. Exit code 0 iff every endpoint passed.

set -uo pipefail

base_url="${1:?usage: check-endpoints.sh <base-url> [annotation-title] [results-file]}"
title="${2:-Endpoints}"
results_file="${3:-/tmp/check-endpoints-results.md}"

# name ^ path+query ^ jq assertion on the body ^ expected Cache-Control
#
# Field separator is `^`, NOT `|` — jq expressions use `|` as their own pipe
# operator, and using it here would silently truncate the assertion and the
# expected header.
checks=$(cat <<'CHECKS'
api-categories^api-categories?limit=1^type == "array"^public, max-age=86400
api-sources^api-sources?limit=1^type == "array"^public, max-age=3600
api-articles^api-articles?limit=1^type == "array"^public, max-age=900, stale-while-revalidate=1800
api-search^api-search?q=test&limit=1^type == "array"^private, max-age=60
api-health^api-health^.status == "ok"^no-store
api-source-health^api-source-health^has("summary") and (.summary | has("total"))^public, max-age=60
CHECKS
)

failed=0
results=""
body_file=$(mktemp)
trap 'rm -f "$body_file"' EXIT

while IFS='^' read -r name path assertion want_cc; do
  [ -z "$name" ] && continue

  hdr=$(mktemp)

  # Truncate the body file BEFORE every request: curl does not truncate `-o`
  # when it never connects, so without this a failed endpoint would be
  # re-asserted against the PREVIOUS endpoint's body (the status check still
  # fails the job — this keeps the diagnostics truthful).
  : > "$body_file"

  # No -f: a non-2xx must be reported as a status mismatch, not swallowed as
  # a curl error. On transport failure curl still prints 000 via -w and exits
  # non-zero — keep the 000, absorb the exit code, and never let one dead
  # endpoint abort the loop. (A bare `|| echo "000"` would append a second
  # 000 and report status=000000.)
  status=$(curl -sS --max-time 20 -D "$hdr" -w '%{http_code}' -o "$body_file" "${base_url}/${path}" 2>/dev/null) || true
  status="${status:-000}"
  body=$(cat "$body_file" 2>/dev/null || true)

  # Header names are case-insensitive; normalise, then collapse the header's
  # internal whitespace so ", " vs "," never fails a match.
  got_cc=$(grep -i '^cache-control:' "$hdr" | tail -1 \
    | cut -d: -f2- | tr -d '\r' | sed -E 's/^[[:space:]]+//; s/[[:space:]]+$//; s/,[[:space:]]*/, /g')
  rm -f "$hdr"

  problems=""
  if [ "$status" != "200" ] && [ "$status" != "206" ]; then
    problems="${problems} status=${status} (want 200 or 206);"
  fi

  if ! printf '%s' "$body" | jq -e "$assertion" >/dev/null 2>&1; then
    problems="${problems} body failed assertion \`${assertion}\`;"
  fi

  if [ "$got_cc" != "$want_cc" ]; then
    problems="${problems} Cache-Control='${got_cc}' (want '${want_cc}');"
  fi

  if [ -n "$problems" ]; then
    failed=1
    echo "::error title=${title}::${name} —${problems}"
    # Body may contain an upstream error message; cap it so a large payload
    # can't flood the log.
    echo "  body (first 300 chars): $(printf '%s' "$body" | head -c 300)"
    results="${results}| \`${name}\` | ❌ |${problems} |"$'\n'
  else
    echo "${name}: OK (${status}, shape ok, Cache-Control ok)"
    results="${results}| \`${name}\` | ✅ | ${status} · shape ok · \`${want_cc}\` |"$'\n'
  fi
done <<< "$checks"

{
  echo "| Endpoint | Result | Detail |"
  echo "|----------|--------|--------|"
  printf '%s' "$results"
} > "$results_file"

if [ "$failed" -ne 0 ]; then
  echo "::error title=${title}::one or more endpoints failed"
  exit 1
fi
echo "All 6 endpoints passed."
