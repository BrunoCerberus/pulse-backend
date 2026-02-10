# Fix All 10 Codebase Weaknesses

## Phase 1: Foundation
- [x] Step 1: `httputil/transport.go` — Add `NewClientWithRedirectLimit` (#6 foundation)
- [x] Step 2: `database/supabase.go` — Add `readErrorBody` helper + `log.Printf` (#2, #7)

## Phase 2: Parser Layer
- [x] Step 3: `parser/ogimage.go` — Use `NewClientWithRedirectLimit` (#6)
- [x] Step 4: `parser/content.go` — Use `NewClientWithRedirectLimit` + drain body (#4, #6)

## Phase 3: Edge Functions
- [x] Step 5: `supabase-proxy.ts` — Remove deprecated `proxyToSupabase` (#3)

## Phase 4: Parser Refactoring
- [x] Step 6: `parser/parser.go` — Extract helpers from `itemToArticle` (#10)

## Phase 5: Main Entry Point
- [x] Step 7: `main.go` — Constants, error logging, Store interface, backfill generics (#1, #5, #8, #9)

## Verification
- [x] `go build ./...` compiles
- [x] `make test-go` passes (85 tests)
- [x] `make test-go-race` passes (0 data races)
- [x] Grep for removed patterns — all clean
