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
COMPOSE := sh scripts/container.sh compose
GOOSE := go tool goose -dir migrations postgres "$$RONOTE_DATABASE_URL"
db_url_required = test -n "$$RONOTE_DATABASE_URL" || { echo "RONOTE_DATABASE_URL is not set; copy .env.example to .env.dev" >&2; exit 1; }
WEB := pnpm --dir apps/web
node_required = want=$$(cat .node-version); have=$$(node --version 2>/dev/null); [ "$$have" = "v$$want" ] || \
	{ echo "Node $$want is required (.node-version), found $${have:-none}. With nvm: nvm install $$want && nvm use $$want" >&2; exit 1; }

help: ## Show available targets
	@awk 'BEGIN { FS = ":.*## " } /^[a-z][a-z0-9-]*:.*## / { printf "  %-18s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

setup: ## Install the commit hook and rebase on pull
	git config core.hooksPath .githooks
	chmod +x .githooks/commit-msg
	git config pull.rebase true

hooks-test: ## Test the commit message hook and scripts/check-commits.sh
	sh scripts/test-commit-msg.sh
	sh scripts/test-check-commits.sh

check-commits: ## Check commit messages in RANGE (in CI the event sets the range)
	@range='$(RANGE)'; [ -n "$$range" ] || range=$$(sh scripts/commit-range.sh) || exit 1; \
		sh scripts/check-commits.sh "$$range"

tools: ## Install the pinned golangci-lint into ./bin
	@$(golangci_lint_current) || { \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/v$(GOLANGCI_LINT_VERSION)/install.sh | \
			sh -s -- -b bin v$(GOLANGCI_LINT_VERSION); \
		$(golangci_lint_current) || { echo "make tools: failed to install golangci-lint $(GOLANGCI_LINT_VERSION)" >&2; exit 1; }; \
	}

check: lint test hooks-test ## Run what CI's fast jobs run; do this before pushing

test: test-go test-web ## Run Go and web unit tests

test-go: ## Run Go unit tests with the race detector
	go test -race -count=1 ./...

test-web: ## Run web unit tests (Vitest)
	@$(node_required)
	$(WEB) test

run-api: ## Run the API locally (RONOTE_ENV defaults to dev)
	RONOTE_ENV=$${RONOTE_ENV:-dev} go run ./cmd/api

lint: lint-go lint-web ## Lint Go and web code

lint-go: ## Lint Go code and CI workflows (golangci-lint, deadcode, actionlint)
	@$(golangci_lint_required)
	$(GOLANGCI_LINT) run
	@dead=$$(go tool deadcode -test -tags integration ./...) && [ -z "$$dead" ] || \
		{ printf '%s\n' "$$dead" >&2; echo "make lint-go: deadcode failed or found unreachable functions" >&2; exit 1; }
	go tool actionlint

lint-web: ## Lint web code (Biome, tsc, knip)
	@$(node_required)
	$(WEB) lint

fmt: ## Format Go code with gofumpt and goimports
	@$(golangci_lint_required)
	$(GOLANGCI_LINT) fmt

dev-up: ## Start the dev stack (Postgres) and wait until it is healthy
	@[ -f .env.dev ] || { cp .env.example .env.dev; echo "make dev-up: created .env.dev from .env.example"; }
	$(COMPOSE) up -d --wait

dev-down: ## Stop the dev stack; the data volume is kept
	$(COMPOSE) down

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

sqlc: ## Regenerate internal/store/sqlcdb from migrations and queries
	go tool sqlc generate

test-integration: ## Run unit and integration tests against throwaway Postgres containers
	sh scripts/container.sh test go test -race -count=1 -tags integration ./...

test-clean: ## Remove leftover testcontainers containers
	sh scripts/container.sh clean

web-install: ## Install web dependencies and the Playwright browser
	@$(node_required)
	pnpm install --frozen-lockfile
	$(WEB) exec playwright install chromium

web-dev: ## Run the web dev server (styleguide at /_styleguide)
	@$(node_required)
	$(WEB) dev

web-build: ## Build the web app; fails if dev-only code ships or it is over budget
	@$(node_required)
	$(WEB) build
	@! grep -rIil styleguide apps/web/dist || \
		{ echo "make web-build: dev-only styleguide code found in the files above" >&2; exit 1; }
	sh scripts/web-budget.sh

web-e2e: ## Run web end-to-end tests (Playwright, Chromium)
	@$(node_required)
	$(WEB) e2e

.PHONY: help setup hooks-test check-commits tools check test test-go test-web run-api lint lint-go lint-web fmt
.PHONY: dev-up dev-down dev-logs db-shell migrate-up migrate-down migrate-status migrate-new sqlc
.PHONY: test-integration test-clean web-install web-dev web-build web-e2e
