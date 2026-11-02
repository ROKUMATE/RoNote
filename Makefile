help: ## Show available targets
	@awk 'BEGIN { FS = ":.*## " } /^[a-z][a-z-]*:.*## / { printf "  %-12s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

setup: ## Point git at .githooks and make the hook executable
	git config core.hooksPath .githooks
	chmod +x .githooks/commit-msg

hooks-test: ## Test the commit message hook
	sh scripts/test-commit-msg.sh

.PHONY: help setup hooks-test
