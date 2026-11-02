GOLANGCI_LINT_VERSION := 2.15.0
GOLANGCI_LINT := bin/golangci-lint
golangci_lint_current = [ "$$($(GOLANGCI_LINT) version --short 2>/dev/null)" = "$(GOLANGCI_LINT_VERSION)" ]

help: ## Show available targets
	@awk 'BEGIN { FS = ":.*## " } /^[a-z][a-z-]*:.*## / { printf "  %-12s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

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

.PHONY: help setup hooks-test tools test run-api
