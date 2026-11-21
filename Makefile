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

hooks-test: ## Test the commit message hook, scripts/check-commits.sh, the doctor checks, run/stop, pdftext's arithmetic and the installer's environment check
	PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s deploy/pdftext -t deploy/pdftext
	sh scripts/test-commit-msg.sh
	sh scripts/test-check-commits.sh
	sh scripts/test-container-clock.sh
	sh scripts/test-embed-check.sh
	sh scripts/test-pdf-check.sh
	sh scripts/test-run-stop.sh
	sh scripts/test-doc-sizes.sh
	sh scripts/test-check-environment.sh

check-commits: ## Check commit messages in RANGE (in CI the event sets the range)
	@range='$(RANGE)'; [ -n "$$range" ] || range=$$(sh scripts/commit-range.sh) || exit 1; \
		sh scripts/check-commits.sh "$$range"

tools: ## Install the pinned golangci-lint into ./bin
	@$(golangci_lint_current) || { \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/v$(GOLANGCI_LINT_VERSION)/install.sh | \
			sh -s -- -b bin v$(GOLANGCI_LINT_VERSION); \
		$(golangci_lint_current) || { echo "make tools: failed to install golangci-lint $(GOLANGCI_LINT_VERSION)" >&2; exit 1; }; \
	}

check: lint test test-perf hooks-test units-check ## Run what CI's fast jobs run; do this before pushing

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

run-llm: ## Run the fake chat model on 127.0.0.1:8094 (dev only); make stop ends it
	sh scripts/run.sh llm go run ./cmd/fakellm

pdf-fixtures: ## Print the PDF import's fixtures into testdata/pdf (Chromium, qpdf); make pdf-record after
	@$(node_required)
	$(WEB) exec node scripts/pdf-fixtures.ts ../..
	qpdf --encrypt ronote-fixture owner-fixture 256 -- testdata/pdf/real-python-controlflow.pdf testdata/pdf/encrypted.pdf

pdf-record: ## Record docling's and pdftext's answers for every PDF fixture, then the IR goldens and report (make dev-up-docling first); local only
	go test ./internal/fakedocling -run TestRecordings -record -count=1 -timeout 30m
	go test ./internal/docling -run 'TestFixturesMapToGoldens|TestPDFCodeIsVerbatim' -update -count=1

run-docling: ## Run the fake docling and pdftext on 127.0.0.1:5001 and :5002 (dev only); make stop ends it
	sh scripts/run.sh docling go run ./cmd/fakedocling

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
	@! go list -deps ./cmd/api ./cmd/worker | grep -q testcontainers || \
		{ echo "make lint-go: cmd/api or cmd/worker links testcontainers; only tests and cmd/eval may" >&2; exit 1; }
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

doctor: ## Check the local setup: the VM's clock is the host's, the embedder serves the pinned model, and the blob store, docling and pdftext answer
	sh scripts/container.sh clock
	sh scripts/embed-check.sh
	sh scripts/pdf-check.sh

dev-up: ## Start the dev stack (Postgres, SeaweedFS with its bucket) and wait until it is healthy
	@[ -f .env.dev ] || { cp .env.example .env.dev; echo "make dev-up: created .env.dev from .env.example"; }
	$(COMPOSE) up -d --wait

dev-up-docling: ## Start the dev stack with the real docling and pdftext, sandboxed, on 127.0.0.1:5001 and :5002 (a 5 GB image, 6 GB of memory)
	@[ -f .env.dev ] || { cp .env.example .env.dev; echo "make dev-up-docling: created .env.dev from .env.example"; }
	$(COMPOSE) --profile docling up -d --wait

dev-down: stop ## Stop the dev processes and the dev stack, docling included; the data volumes are kept
	$(COMPOSE) --profile docling down

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

eval: ## Measure search on the retrieval eval with recorded vectors; fails below baseline.json (no model needed)
	sh scripts/container.sh test go run ./cmd/eval

eval-record: ## Embed the eval's corpus and queries through RONOTE_EMBED_URL and record the vectors (needs the model)
	sh scripts/container.sh test go run ./cmd/eval -record

eval-review: ## List the eval's queries marked for review: expected blocks and hybrid's top 5 (writes nothing)
	sh scripts/container.sh test go run ./cmd/eval -review

eval-chat: ## Ask the eval's queries through chat, of the real model; writes chat-report.md from a full run (local only: RONOTE_ANTHROPIC_API_KEY, the embedder, cents). [limit=N] [ids=e06,n04 [repeat=N]] [budget=dollars, default 1.50]
	sh scripts/container.sh test go run ./cmd/eval -chat -chat-limit=$(or $(limit),0) -chat-ids=$(ids) -chat-repeat=$(or $(repeat),1) -chat-budget=$(or $(budget),1.50)

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

web-dev: ## Run the web dev server (styleguide at /_styleguide), the runner, the fake sign-in, chat model and docling; make stop ends them
	@$(node_required)
	sh scripts/run.sh web sh -c 'go run ./cmd/fakeidp & idp=$$!; go run ./cmd/fakellm & llm=$$!; go run ./cmd/fakedocling & docling=$$!; $(WEB) runner & runner=$$!; $(WEB) dev; kill $$idp $$llm $$docling $$runner'

web-build: ## Build the web app and the runner; fails if dev-only code ships or it is over budget
	@$(node_required)
	$(WEB) build
	@! grep -rIil styleguide apps/web/dist || \
		{ echo "make web-build: dev-only styleguide code found in the files above" >&2; exit 1; }
	$(WEB) exec node scripts/budget.ts

web-e2e: ## Run web end-to-end tests against the full stack (Playwright, Chromium); locally needs make dev-up
	@$(node_required)
	RONOTE_E2E_DATABASE_URL="$(E2E_DATABASE_URL)" sh scripts/e2e-db.sh
	go build -o bin/e2e/ ./cmd/api ./cmd/worker ./cmd/fakeidp ./cmd/fakeembed ./cmd/fakellm ./cmd/fakedocling
	RONOTE_E2E_DATABASE_URL="$(E2E_DATABASE_URL)" $(WEB) e2e

# Images (deploy/images, docs/adr/0014-production-topology.md): VERSION is
# stamped into the binaries; the local images are tagged IMAGE_TAG, under
# the names the units pull, so installing tag local uses them.
VERSION ?= $(shell git describe --tags --always --dirty)
IMAGE_TAG ?= local
GO_IMAGE = podman build -f deploy/images/Containerfile.binary --build-arg VERSION=$(VERSION)
IMAGES = ghcr.io/rokumate/ronote-api:$(IMAGE_TAG) ghcr.io/rokumate/ronote-worker:$(IMAGE_TAG) ghcr.io/rokumate/ronote-web:$(IMAGE_TAG) localhost/ronote-fakes:$(IMAGE_TAG)

images: ## Build the api, worker, web and fakes images with podman, and print their sizes
	$(GO_IMAGE) --build-arg BINARY=api -t ghcr.io/rokumate/ronote-api:$(IMAGE_TAG) .
	$(GO_IMAGE) --build-arg BINARY=worker -t ghcr.io/rokumate/ronote-worker:$(IMAGE_TAG) .
	podman build -f deploy/images/Containerfile.web -t ghcr.io/rokumate/ronote-web:$(IMAGE_TAG) .
	podman build -f deploy/images/Containerfile.fakes -t localhost/ronote-fakes:$(IMAGE_TAG) .
	for i in $(IMAGES); do podman images --format '{{.Repository}}:{{.Tag}} {{.Size}}' $$i; done

# The production units with the fakes (deploy/quadlet/ci): the configuration
# and pages install.sh gets, and the images, as an archive.
PROD_CONFIG = bin/prod-config
SMOKE_CA = bin/smoke-ca.pem

prod-config: images
	rm -rf $(PROD_CONFIG)
	mkdir -p $(PROD_CONFIG)/fixtures/pages/real-python-controlflow $(PROD_CONFIG)/fixtures/linked/python
	cp deploy/quadlet/ci/config/* $(PROD_CONFIG)/
	cp testdata/golden/real-python-controlflow/input.html $(PROD_CONFIG)/fixtures/pages/real-python-controlflow/index.html
	cp apps/web/e2e/fixtures/python.html $(PROD_CONFIG)/fixtures/linked/python/index.html
	rm -f bin/ronote-images.tar
	podman save --multi-image-archive -o bin/ronote-images.tar $(IMAGES)

prod-ci: prod-config ## Install the production units with the fakes on this Linux host (sudo), and check only 80 and 443 answer
	sudo env RONOTE_GITHUB_CLIENT_SECRET=ronote-e2e-secret sh deploy/install.sh --version $(IMAGE_TAG) --config $(PROD_CONFIG) --drop-ins deploy/quadlet/ci --images bin/ronote-images.tar --skip-model
	sudo sh deploy/as-ronote.sh podman exec ronote-caddy cat /caddy/data/caddy/pki/authorities/local/intermediate.crt > $(SMOKE_CA)
	sh deploy/check-ports.sh 127.0.0.1 $$(hostname -I | cut -d' ' -f1)

prod-local: prod-config ## The same in the podman machine's VM, then the smoke suite from this Mac
	sh scripts/prod-local.sh

prod-smoke: ## Run the smoke suite through Caddy against installed units (make prod-ci first)
	RONOTE_SMOKE_CA=$(CURDIR)/$(SMOKE_CA) $(WEB) exec playwright test --config playwright.smoke.config.ts

# check-config on deploy/ronote.env.example, with stand-ins for its secrets:
# it must pass, and fail with the runner on the app's site.
EXAMPLE_SECRETS = -e RONOTE_DATABASE_URL=postgres://ronote:example@ronote-postgres:5432/ronote \
	-e RONOTE_GITHUB_CLIENT_SECRET=example -e RONOTE_ANTHROPIC_API_KEY=example \
	-e RONOTE_BLOB_ACCESS_KEY=example -e RONOTE_BLOB_SECRET_KEY=example \
	-e RONOTE_DOCLING_API_KEY=example -e RONOTE_PDFTEXT_API_KEY=example
CHECK_CONFIG = podman run --rm --env-file deploy/ronote.env.example $(EXAMPLE_SECRETS)

units-check: ## Convert the Quadlet units with Quadlet's dry run (skipped where Quadlet isn't installed)
	sh deploy/units-check.sh

prod-check-config: ## Check the example production config with the api image; a same-site runner must fail
	$(CHECK_CONFIG) ghcr.io/rokumate/ronote-api:$(IMAGE_TAG) check-config
	! $(CHECK_CONFIG) -e RONOTE_RUNNER_URL=https://run.ronote.example ghcr.io/rokumate/ronote-api:$(IMAGE_TAG) check-config

.PHONY: help setup hooks-test check-commits tools check test test-go test-perf test-web run-api run-worker run-idp run-llm run-docling pdf-fixtures pdf-record extract lint lint-go lint-web fmt
.PHONY: doctor stop dev-up dev-up-docling dev-down dev-logs db-shell migrate-up migrate-down migrate-status migrate-new river-migration sqlc
.PHONY: generate check-generated check-go-version ci-cache-keys
.PHONY: test-integration eval eval-record eval-review eval-chat test-perf-search test-live test-clean web-install web-deps web-browsers check-node-version
.PHONY: web-dev web-build web-e2e images units-check prod-config prod-ci prod-local prod-smoke prod-check-config
