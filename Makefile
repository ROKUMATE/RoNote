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

help: ## Show available targets
	@awk 'BEGIN { FS = ":.*## " } /^[a-z][a-z-]*:.*## / { printf "  %-18s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

setup: ## Install the commit hook and rebase on pull
	git config core.hooksPath .githooks
	chmod +x .githooks/commit-msg
	git config pull.rebase true

hooks-test: ## Test the commit message hook
	sh scripts/test-commit-msg.sh

tools: ## Install the pinned golangci-lint into ./bin
	@$(golangci_lint_current) || { \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/v$(GOLANGCI_LINT_VERSION)/install.sh | \
			sh -s -- -b bin v$(GOLANGCI_LINT_VERSION); \
		$(golangci_lint_current) || { echo "make tools: failed to install golangci-lint $(GOLANGCI_LINT_VERSION)" >&2; exit 1; }; \
	}

test: ## Run unit tests with the race detector
	go test -race -count=1 ./...

run-api: ## Run the API locally (RONOTE_ENV defaults to dev)
	RONOTE_ENV=$${RONOTE_ENV:-dev} go run ./cmd/api

lint: ## Run golangci-lint, then fail if deadcode reports anything
	@$(golangci_lint_required)
	$(GOLANGCI_LINT) run
	@dead=$$(go tool deadcode -test ./...) && [ -z "$$dead" ] || \
		{ printf '%s\n' "$$dead" >&2; echo "make lint: deadcode failed or found unreachable functions" >&2; exit 1; }

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

.PHONY: help setup hooks-test tools test run-api lint fmt dev-up dev-down dev-logs db-shell
