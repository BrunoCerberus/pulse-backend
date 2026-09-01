.PHONY: help test test-go test-go-cover test-go-race test-deno fuzz build run clean deploy deploy-all deploy-categories deploy-sources deploy-articles deploy-search deploy-health functions-serve cleanup backfill-images backfill-content

# Default target
help:
	@echo "Pulse Backend - Available Commands"
	@echo ""
	@echo "Testing:"
	@echo "  make test            Run all tests (Go + Deno)"
	@echo "  make test-go         Run Go tests"
	@echo "  make test-go-cover   Run Go tests with coverage"
	@echo "  make test-go-race    Run Go tests with race detector"
	@echo "  make test-deno       Run Deno Edge Function tests"
	@echo "  make fuzz            Fuzz every target (30s each; FUZZTIME=5m to extend)"
	@echo ""
	@echo "Build & Run:"
	@echo "  make build           Build the RSS worker binary"
	@echo "  make run             Run the RSS worker (fetch feeds)"
	@echo "  make cleanup         Run cleanup (remove old articles)"
	@echo "  make backfill-images Run og:image backfill"
	@echo "  make backfill-content Run content backfill"
	@echo ""
	@echo "Supabase Functions:"
	@echo "  make deploy          Deploy all Edge Functions"
	@echo "  make deploy-categories  Deploy api-categories function"
	@echo "  make deploy-sources     Deploy api-sources function"
	@echo "  make deploy-articles    Deploy api-articles function"
	@echo "  make deploy-search      Deploy api-search function"
	@echo "  make deploy-health      Deploy api-health function"
	@echo "  make functions-serve    Run Edge Functions locally"
	@echo ""
	@echo "Utilities:"
	@echo "  make clean           Remove build artifacts"

# =============================================================================
# Testing
# =============================================================================

test: test-go test-deno
	@echo "All tests passed!"

test-go:
	cd rss-worker && go test -v ./...

test-go-cover:
	cd rss-worker && go test -cover ./...

test-go-race:
	cd rss-worker && go test -v -race ./...

# Mirrors fuzz.yml target discovery: find every `func FuzzXxx` and run it, since
# `go test` accepts only one -fuzz target per invocation. Override the budget with
# `make fuzz FUZZTIME=5m`.
FUZZTIME ?= 30s
fuzz:
	@cd rss-worker && \
	found=0; \
	for file in $$(grep -rl --include='*_test.go' -E '^func Fuzz[A-Za-z0-9_]*\(' .); do \
		pkg=$$(dirname "$$file"); pkg=$${pkg#./}; \
		for target in $$(grep -oE '^func Fuzz[A-Za-z0-9_]*' "$$file" | sed -E 's/^func //'); do \
			found=$$((found + 1)); \
			echo "==> $$target ($$pkg, $(FUZZTIME))"; \
			go test -run '^$$' -fuzz "^$$target$$" -fuzztime $(FUZZTIME) "./$$pkg" || exit 1; \
		done; \
	done; \
	[ "$$found" -gt 0 ] || { echo "no fuzz targets found"; exit 1; }; \
	echo "$$found fuzz target(s) passed."

test-deno:
	cd supabase/functions && deno test --allow-env --allow-net _shared/ api-articles/ api-categories/ api-sources/ api-search/ api-health/ api-source-health/

# =============================================================================
# Build & Run
# =============================================================================

build:
	cd rss-worker && go mod tidy && go build -o rss-worker .

run:
	cd rss-worker && go run .

cleanup:
	cd rss-worker && go run . cleanup

backfill-images:
	cd rss-worker && go run . backfill-images

backfill-content:
	cd rss-worker && go run . backfill-content

clean:
	rm -f rss-worker/rss-worker
	rm -f rss-worker/coverage.out

# =============================================================================
# Supabase Edge Functions
# =============================================================================

deploy: deploy-all

deploy-all:
	supabase functions deploy

deploy-categories:
	supabase functions deploy api-categories

deploy-sources:
	supabase functions deploy api-sources

deploy-articles:
	supabase functions deploy api-articles

deploy-search:
	supabase functions deploy api-search

deploy-health:
	supabase functions deploy api-health

functions-serve:
	supabase functions serve
