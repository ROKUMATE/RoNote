# Local settings: plain KEY=value lines. They override the shell environment;
# make VAR=value still wins.
ifneq ($(wildcard .env.dev),)
include .env.dev
export $(shell sed -n 's/^\([A-Za-z_][A-Za-z0-9_]*\)=.*/\1/p' .env.dev)
endif

GOLANGCI_LINT_VERSION := 2.15.0
GOLANGCI_LINT := bin/golangci-lint
golangci_lint_current = [ "$$($(GOLANGCI_LINT) version --short 2>/dev/null)" = "$(GOLANGCI_LINT_VERSION)" ]
golangci_lint_required = $(golangci_lint_current) || { echo "$(GOLANGCI_LINT) $(GOLANGCI_LINT_VERSION) not found; run make tools" >&2; exit 1; }
go_toolchain = sed -n 's/^toolchain //p' go.mod
COMPOSE := sh scripts/container.sh compose
GOOSE := go tool goose -dir migrations postgres "$$RONOTE_DATABASE_URL"
db_url_required = test -n "$$RONOTE_DATABASE_URL" || { echo "RONOTE_DATABASE_URL is not set; copy .env.example to .env.dev" >&2; exit 1; }
WEB := pnpm --dir apps/web
# The end-to-end tests' own database, recreated by every make web-e2e.
E2E_DATABASE_URL ?= postgres://ronote:ronote@127.0.0.1:5432/ronote_e2e?sslmode=disable
GENERATED := internal/store/sqlcdb internal/httpapi/gen apps/web/src/api/schema.d.ts
node_required = want=$$(cat .nvmrc); have=$$(node --version 2>/dev/null); [ "$$have" = "v$$want" ] || \
	{ echo "Node $$want is required (.nvmrc), found $${have:-none}. With nvm: nvm install && nvm use" >&2; exit 1; }

help: ## Show available targets
	@awk 'BEGIN { FS = ":.*## " } /^[a-z][a-z0-9-]*:.*## / { printf "  %-18s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

setup: ## Install the commit hook and rebase on pull
	git config core.hooksPath .githooks
	chmod +x .githooks/commit-msg
	git config pull.rebase true

hooks-test: ## Test the commit message hook, scripts/check-commits.sh, the doctor checks and run/stop
	sh scripts/test-commit-msg.sh
	sh scripts/test-check-commits.sh
	sh scripts/test-container-clock.sh
	sh scripts/test-embed-check.sh
	sh scripts/test-run-stop.sh
	sh scripts/test-doc-sizes.sh

check-commits: ## Check commit messages in RANGE (in CI the event sets the range)
	@range='$(RANGE)'; [ -n "$$range" ] || range=$$(sh scripts/commit-range.sh) || exit 1; \
		sh scripts/check-commits.sh "$$range"

tools: ## Install the pinned golangci-lint into ./bin
	@$(golangci_lint_current) || { \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/v$(GOLANGCI_LINT_VERSION)/install.sh | \
			sh -s -- -b bin v$(GOLANGCI_LINT_VERSION); \
		$(golangci_lint_current) || { echo "make tools: failed to install golangci-lint $(GOLANGCI_LINT_VERSION)" >&2; exit 1; }; \
	}

check: lint test test-perf hooks-test ## Run what CI's fast jobs run; do this before pushing

test: test-go test-web ## Run Go and web unit tests

test-go: ## Run Go unit tests with the race detector
	go test -race -count=1 ./...

test-perf: ## Run timing tests (perf build tag) alone: one package at a time, without the race detector
	go test -tags perf -p 1 -count=1 -run '^TestPerf' ./...

test-web: ## Run web unit tests (Vitest)
	@$(node_required)
	$(WEB) test

run-api: ## Run the API locally (RONOTE_ENV defaults to dev); make stop ends it
	RONOTE_ENV=$${RONOTE_ENV:-dev} sh scripts/run.sh api go run ./cmd/api

run-worker: ## Run the import worker locally (RONOTE_ENV defaults to dev); make stop ends it
	RONOTE_ENV=$${RONOTE_ENV:-dev} sh scripts/run.sh worker go run ./cmd/worker

run-idp: ## Run the fake GitHub sign-in on 127.0.0.1:8090 (dev only); make stop ends it
	sh scripts/run.sh idp go run ./cmd/fakeidp

extract: ## Print a page's IR: make extract url=https://... [format=text]
	@test -n "$(url)" || { echo "usage: make extract url=https://... [format=text]" >&2; exit 1; }
	go run ./cmd/extract --format=$(or $(format),json) "$(url)"

lint: lint-go lint-web lint-docs ## Lint Go and web code, and the agent docs' sizes

lint-go: ## Lint Go code and CI workflows (golangci-lint, deadcode, go mod tidy, actionlint)
	@$(golangci_lint_required)
	$(GOLANGCI_LINT) run
	@dead=$$(go tool deadcode -test -tags integration,live,perf ./...) || { echo "make lint-go: deadcode failed" >&2; exit 1; }; \
		dead=$$(printf '%s\n' "$$dead" | grep -v '^internal/httpapi/gen/' || true); \
		[ -z "$$dead" ] || { printf '%s\n' "$$dead" >&2; echo "make lint-go: deadcode found unreachable functions" >&2; exit 1; }
	go mod tidy -diff
	go tool actionlint

lint-docs: ## Fail when CONTEXT.md or AGENTS.md outgrows its size (80 lines and 6 KB; 150 lines and 10 KB)
	sh scripts/check-doc-sizes.sh

lint-web: ## Lint web code (Biome, tsc, knip)
	@$(node_required)
	$(WEB) lint

fmt: ## Format Go code with gofumpt and goimports
	@$(golangci_lint_required)
	$(GOLANGCI_LINT) fmt

doctor: ## Check the local setup: the container VM's clock is the host's, and the embedder serves the pinned model
	sh scripts/container.sh clock
	sh scripts/embed-check.sh

dev-up: ## Start the dev stack (Postgres) and wait until it is healthy
	@[ -f .env.dev ] || { cp .env.example .env.dev; echo "make dev-up: created .env.dev from .env.example"; }
	$(COMPOSE) up -d --wait

dev-down: stop ## Stop the dev processes and the dev stack; the data volume is kept
	$(COMPOSE) down

stop: ## Stop what run-api, run-worker and web-dev started, with everything they started
	@sh scripts/stop.sh

dev-logs: ## Follow the dev stack logs
	$(COMPOSE) logs -f

db-shell: ## Open psql on the dev database
	$(COMPOSE) exec postgres psql -U ronote -d ronote

migrate-up: ## Apply all pending migrations
	@$(db_url_required)
	$(GOOSE) up

migrate-down: ## Roll back the latest migration
	@$(db_url_required)
	$(GOOSE) down

migrate-status: ## Show which migrations are applied
	@$(db_url_required)
	$(GOOSE) status

migrate-new: ## Create a SQL migration: make migrate-new name=add_documents
	@test -n "$(name)" || { echo "usage: make migrate-new name=add_documents" >&2; exit 1; }
	go tool goose -dir migrations -s create $(name) sql

river-migration: ## Add a migration with River's schema: make river-migration first=2 last=8
	@test -n "$(first)" && test -n "$(last)" || { echo "usage: make river-migration first=2 last=8" >&2; exit 1; }
	@file=$$(go tool goose -dir migrations -s create river_$(first)_to_$(last) sql 2>&1 | sed -n 's/.*Created new file: //p'); \
		test -n "$$file" || { echo "make river-migration: goose did not create a file" >&2; exit 1; }; \
		sh scripts/river-migration.sh $(first) $(last) >"$$file" || { rm -f "$$file"; exit 1; }; \
		echo "make river-migration: wrote $$file"

sqlc: ## Regenerate internal/store/sqlcdb from migrations and queries
	go tool sqlc generate

generate: sqlc ## Regenerate all generated code: sqlc, then the API server and the web app's API types from api/openapi.yaml
	go tool oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml
	@$(node_required)
	pnpm --filter @ronote/openapi-ts generate

check-generated: generate ## Fail if generated code is stale
	@test -z "$$(git status --porcelain -- $(GENERATED))" || \
		{ git status --short -- $(GENERATED); echo "make check-generated: generated code is stale; run make generate and commit it" >&2; exit 1; }

check-go-version: ## Print the Go toolchain; fail unless it is the one go.mod pins
	@want=$$($(go_toolchain)); have=$$(go env GOVERSION); \
		[ "$$have" = "$$want" ] || { echo "Go toolchain $$want is required (go.mod), found $$have" >&2; exit 1; }; \
		echo "Go toolchain $$have, as pinned in go.mod"

ci-cache-keys: ## (CI) Write tool versions used in cache keys to GITHUB_OUTPUT
	@: "$${GITHUB_OUTPUT:?ci-cache-keys writes to GITHUB_OUTPUT, which only GitHub Actions sets}"
	@echo "golangci_lint=$(GOLANGCI_LINT_VERSION)" >>"$$GITHUB_OUTPUT"
	@sed -n 's/.*"@playwright\/test": "\([^"]*\)".*/playwright=\1/p' apps/web/package.json >>"$$GITHUB_OUTPUT"

test-integration: ## Run unit and integration tests against throwaway Postgres containers
	sh scripts/container.sh test go test -race -count=1 -tags integration ./...

test-live: ## Extract real pages over the network, print a report, and check the RAG tutorial runs nothing (never in CI)
	@$(node_required)
	go test -tags live -run TestLive -count=1 -v ./internal/extract
	$(WEB) live

test-perf-search: ## Time search over 5k-25k chunks for one user, among 200,000 others (local only: minutes, and a container)
	sh scripts/container.sh test go test -tags perf,integration -p 1 -count=1 -timeout 30m -run '^TestPerfSearch' -v ./internal/search

test-clean: ## Remove leftover testcontainers containers
	sh scripts/container.sh clean

web-install: web-deps web-browsers ## Install web dependencies and the Playwright browser

web-deps: ## Install web dependencies from the lockfile
	@$(node_required)
	pnpm install --frozen-lockfile

web-browsers: ## Install Playwright's Chromium (WITH_DEPS=1 adds its system packages, for CI)
	@$(node_required)
	$(WEB) exec playwright install $(if $(WITH_DEPS),--with-deps) chromium

check-node-version: ## Print the Node version; fail unless it is the one .nvmrc pins
	@$(node_required); echo "Node $$(node --version), as pinned in .nvmrc"

web-dev: ## Run the web dev server (styleguide at /_styleguide), the runner and the fake sign-in; make stop ends them
	@$(node_required)
	sh scripts/run.sh web sh -c 'go run ./cmd/fakeidp & idp=$$!; $(WEB) runner & runner=$$!; $(WEB) dev; kill $$idp $$runner'

web-build: ## Build the web app and the runner; fails if dev-only code ships or it is over budget
	@$(node_required)
	$(WEB) build
	@! grep -rIil styleguide apps/web/dist || \
		{ echo "make web-build: dev-only styleguide code found in the files above" >&2; exit 1; }
	$(WEB) exec node scripts/budget.ts

web-e2e: ## Run web end-to-end tests against the full stack (Playwright, Chromium); locally needs make dev-up
	@$(node_required)
	RONOTE_E2E_DATABASE_URL="$(E2E_DATABASE_URL)" sh scripts/e2e-db.sh
	go build -o bin/e2e/ ./cmd/api ./cmd/worker ./cmd/fakeidp ./cmd/fakeembed
	RONOTE_E2E_DATABASE_URL="$(E2E_DATABASE_URL)" $(WEB) e2e

.PHONY: help setup hooks-test check-commits tools check test test-go test-perf test-web run-api run-worker run-idp extract lint lint-go lint-web fmt
.PHONY: doctor stop dev-up dev-down dev-logs db-shell migrate-up migrate-down migrate-status migrate-new river-migration sqlc
.PHONY: generate check-generated check-go-version ci-cache-keys
.PHONY: test-integration test-perf-search test-live test-clean web-install web-deps web-browsers check-node-version
.PHONY: web-dev web-build web-e2e
