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
# The end-to-end tests' stack: in CI, the job's Postgres and SeaweedFS
# services; locally, a compose project of its own on ports of its own, so
# a run never touches the developer's stack (AGENTS.md). Its database is
# recreated by every make web-e2e.
E2E_PROJECT = ronote-e2e
ifdef CI
E2E_PG_PORT ?= 5432
E2E_S3_PORT ?= 8333
E2E_STACK =
else
E2E_PG_PORT ?= 55432
E2E_S3_PORT ?= 58333
E2E_STACK = e2e-stack-up
endif
E2E_DATABASE_URL ?= postgres://ronote:ronote@127.0.0.1:$(E2E_PG_PORT)/ronote_e2e?sslmode=disable
E2E_COMPOSE = RONOTE_PG_PORT=$(E2E_PG_PORT) RONOTE_S3_PORT=$(E2E_S3_PORT) $(COMPOSE) -p $(E2E_PROJECT)
GENERATED := internal/store/sqlcdb internal/httpapi/gen apps/web/src/api/schema.d.ts deploy/caddy/runner.caddy
node_required = want=$$(cat .nvmrc); have=$$(node --version 2>/dev/null); [ "$$have" = "v$$want" ] || \
	{ echo "Node $$want is required (.nvmrc), found $${have:-none}. With nvm: nvm install && nvm use" >&2; exit 1; }

help: ## Show available targets
	@awk 'BEGIN { FS = ":.*## " } /^[a-z][a-z0-9-]*:.*## / { printf "  %-18s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

setup: ## Install the commit hook and rebase on pull
	git config core.hooksPath .githooks
	chmod +x .githooks/commit-msg
	git config pull.rebase true

hooks-test: ## Test the commit message hook, scripts/check-commits.sh, the doctor checks, run/stop, pdftext's arithmetic the installer's environment check and failure report, the backups, the alerts, ronote-deploy bootstrap's firewall, AppArmor rule and swap, every target making bin/ in a fresh checkout, the real-image reports and the failure journals
	PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s deploy/pdftext -t deploy/pdftext
	sh scripts/test-commit-msg.sh
	PYTHONDONTWRITEBYTECODE=1 python3 scripts/test_renovate.py
	sh scripts/test-check-commits.sh
	sh scripts/test-container-clock.sh
	sh scripts/test-embed-check.sh
	sh scripts/test-pdf-check.sh
	sh scripts/test-run-stop.sh
	sh scripts/test-doc-sizes.sh
	sh scripts/test-check-environment.sh
	sh scripts/test-report-failed.sh
	sh scripts/test-backup.sh
	sh scripts/test-alerts.sh
	sh scripts/test-deploy.sh
	sh scripts/test-open-web-ports.sh
	sh scripts/test-real-images.sh
	sh scripts/test-prod-journals.sh
	sh scripts/test-pasta-apparmor.sh
	sh scripts/test-host-swap.sh
	sh scripts/test-fresh-bin.sh
	sh scripts/test-doc-paths.sh

check-commits: ## Check commit messages in RANGE (in CI the event sets the range)
	@range='$(RANGE)'; [ -n "$$range" ] || range=$$(sh scripts/commit-range.sh) || exit 1; \
		sh scripts/check-commits.sh "$$range"

# bin/ holds what the build and CI make (tools, logs, the production
# config, reports); it is ignored, so a fresh checkout has none. Every target
# that names it waits for it (| bin), made as the user who runs make, never
# with sudo, so the later steps that run without sudo can still write there
# (scripts/test-fresh-bin.sh checks every target).
bin:
	mkdir -p bin

tools: | bin ## Install the pinned golangci-lint into ./bin
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

lint-go: | bin ## Lint Go code and CI workflows (golangci-lint, deadcode, go mod tidy, actionlint)
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

fmt: | bin ## Format Go code with gofumpt and goimports
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

e2e-stack-up: ## Start the end-to-end tests' own Postgres and SeaweedFS: compose project ronote-e2e, on 127.0.0.1:55432 and :58333
	$(E2E_COMPOSE) up -d --wait

e2e-stack-down: ## Stop the end-to-end tests' stack and remove its data; the dev stack is untouched
	$(E2E_COMPOSE) down -v

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

generate: sqlc ## Regenerate all generated code: sqlc, the API server and the web app's API types from api/openapi.yaml, and Caddy's runner site
	go tool oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml
	@$(node_required)
	pnpm --filter @ronote/openapi-ts generate
	$(WEB) exec node scripts/runner-caddy.ts > deploy/caddy/runner.caddy

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

web-build: ## Build the web app and the runner, and the runner's Pages site; fails if dev-only code ships, it is over budget or the site is wrong
	@$(node_required)
	$(WEB) build
	@! grep -rIil styleguide apps/web/dist || \
		{ echo "make web-build: dev-only styleguide code found in the files above" >&2; exit 1; }
	$(WEB) exec node scripts/budget.ts
	@$(MAKE) --no-print-directory runner-pages APP_URL=https://ronote.example

runner-pages: ## Make the runner's Cloudflare Pages site in apps/web/dist-pages for the app at APP_URL, from make web-build's runner
	@test -n "$(APP_URL)" || { echo "make runner-pages: set APP_URL to the app's https origin" >&2; exit 1; }
	$(WEB) exec node scripts/runner-pages.ts "$(APP_URL)"

# wrangler has its own package and lockfile (deploy/pages), outside the
# workspace, so only a release installs it.
runner-deploy: ## Deploy apps/web/dist-pages to Cloudflare Pages (release only: CLOUDFLARE_API_TOKEN, CLOUDFLARE_ACCOUNT_ID, CLOUDFLARE_PAGES_PROJECT)
	@test -n "$$CLOUDFLARE_PAGES_PROJECT" || { echo "make runner-deploy: set CLOUDFLARE_PAGES_PROJECT" >&2; exit 1; }
	@test -d apps/web/dist-pages || { echo "make runner-deploy: no apps/web/dist-pages; run make runner-pages first" >&2; exit 1; }
	pnpm install --frozen-lockfile --dir deploy/pages
	pnpm --dir deploy/pages run pages

web-e2e: $(E2E_STACK) | bin ## Run web end-to-end tests against the full stack (Playwright, Chromium); locally on their own stack (make e2e-stack-up), never the dev stack
	@$(node_required)
	RONOTE_E2E_DATABASE_URL="$(E2E_DATABASE_URL)" RONOTE_E2E_PROJECT=$(E2E_PROJECT) sh scripts/e2e-db.sh
	go build -o bin/e2e/ ./cmd/api ./cmd/worker ./cmd/fakeidp ./cmd/fakeembed ./cmd/fakellm ./cmd/fakedocling
	RONOTE_E2E_DATABASE_URL="$(E2E_DATABASE_URL)" RONOTE_E2E_BLOB_ENDPOINT=http://127.0.0.1:$(E2E_S3_PORT) $(WEB) e2e

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
# and pages install.sh gets, and the images, as an archive. The units' third-
# party images in CI are pulled by the units' own pins and tagged under local
# names, which the drop-ins use: a saved image keeps no digest reference, so a
# unit pinning one would pull it. Nothing else is pulled (Pull=never).
PROD_CONFIG = bin/prod-config
CI_THIRD_PARTY = postgres seaweedfs
# The backups' images (deploy/backup/images), tagged the same way, which CI's
# configuration names.
CI_BACKUP_IMAGES = restic rclone
CI_TAGS = $(foreach n,$(CI_THIRD_PARTY) $(CI_BACKUP_IMAGES),localhost/ronote-ci/$(n):pinned)
# install.sh's secrets in CI and make prod-local: stand-ins made fresh each
# run, so no secret-shaped value is in the repo (install.sh keeps the first
# ones a host is given, and the fake sign-in reads the api's), and a restic
# repository on the host rather than R2.
random = $(shell od -An -N16 -tx1 /dev/urandom | tr -d ' \n')
CI_INSTALL_ENV = RONOTE_GITHUB_CLIENT_SECRET=$(random) RONOTE_RESTIC_PASSWORD=$(random) \
	RONOTE_BACKUP_REPO=/var/lib/restic/repo RONOTE_BACKUP_ACCESS_KEY=none RONOTE_BACKUP_SECRET_KEY=none \
	RONOTE_NTFY_TOPIC=$(random)
SMOKE_CA = bin/smoke-ca.pem
# The drop-in layers (deploy/quadlet): ci for everything CI changes, then the
# fakes for Ollama, docling and pdftext, or the real images' settings.
CI_FAKES_DROPINS = --drop-ins deploy/quadlet/ci --drop-ins deploy/quadlet/ci-fakes
CI_REAL_DROPINS = --drop-ins deploy/quadlet/ci --drop-ins deploy/quadlet/real
# bootstrap.sh with a throwaway deploy key, and the images, as install.sh gets them.
BOOTSTRAP = rm -f bin/ci-deploy-key bin/ci-deploy-key.pub && \
	ssh-keygen -q -t ed25519 -N '' -C ronote-ci -f bin/ci-deploy-key && \
	sudo env $(CI_INSTALL_ENV) sh deploy/bootstrap.sh --deploy-key bin/ci-deploy-key.pub -- --version $(IMAGE_TAG) --config $(PROD_CONFIG) --images bin/ronote-images.tar
define after_install
sudo sh deploy/as-ronote.sh podman exec ronote-caddy cat /caddy/data/caddy/pki/authorities/local/intermediate.crt > $(SMOKE_CA)
sh deploy/check-ports.sh 127.0.0.1 $$(hostname -I | cut -d' ' -f1)
endef

prod-config: images | bin
	rm -rf $(PROD_CONFIG)
	mkdir -p $(PROD_CONFIG)/fixtures/pages/real-python-controlflow $(PROD_CONFIG)/fixtures/linked/python
	cp deploy/quadlet/ci/config/* $(PROD_CONFIG)/
	cp testdata/golden/real-python-controlflow/input.html $(PROD_CONFIG)/fixtures/pages/real-python-controlflow/index.html
	cp apps/web/e2e/fixtures/python.html $(PROD_CONFIG)/fixtures/linked/python/index.html
	for n in $(CI_THIRD_PARTY); do \
		ref=$$(cat deploy/quadlet/ronote-$$n.container deploy/quadlet/ci/ronote-$$n.container 2>/dev/null | sed -n 's/^Image=//p') && \
		podman pull -q "$$ref" >/dev/null && podman tag "$$ref" localhost/ronote-ci/$$n:pinned || exit 1; \
	done
	for n in $(CI_BACKUP_IMAGES); do \
		ref=$$(sed -n "s/^$$n=//p" deploy/backup/images) && \
		podman pull -q "$$ref" >/dev/null && podman tag "$$ref" localhost/ronote-ci/$$n:pinned || exit 1; \
	done
	rm -f bin/ronote-images.tar
	podman save --multi-image-archive -o bin/ronote-images.tar $(IMAGES) $(CI_TAGS)

prod-ci: prod-config | bin ## Bootstrap this Ubuntu 26.04 host and install the production units with the fakes (sudo), and check only 80 and 443 answer
	$(BOOTSTRAP) $(CI_FAKES_DROPINS) --skip-model
	$(after_install)

# The real docling, pdftext and Ollama, with the fakes only for GitHub's sign-in
# and the chat model (.github/workflows/arm-real.yml): the real embedding model
# is pulled and its digest checked, and the worker's embedder is the real one.
ci-clear-environment: ## On a CI runner: remove the runner's per-user XDG_* lines from /etc/environment before installing (sudo)
	sh scripts/ci-clear-environment.sh

ci-limit-cpus: ## On a CI runner, before installing: put every user's slice, ronote's with it, on 2 CPUs, as on Oracle's host (sudo)
	sh scripts/ci-limit-cpus.sh

prod-real: prod-config | bin ## Install the units with the real docling and Ollama images on this Ubuntu 26.04 host (sudo): minutes of pulling
	grep -v '^RONOTE_EMBED_MODEL=' $(PROD_CONFIG)/ronote.env > $(PROD_CONFIG)/ronote.env.real && mv $(PROD_CONFIG)/ronote.env.real $(PROD_CONFIG)/ronote.env
	$(BOOTSTRAP) $(CI_REAL_DROPINS)
	$(after_install)

prod-real-smoke: | bin ## Import a PDF through the real docling and search it with the real embedder, through Caddy (make prod-real first)
	RONOTE_SMOKE_SPEC=real RONOTE_SMOKE_CA=$(CURDIR)/$(SMOKE_CA) $(WEB) exec playwright test --config playwright.smoke.config.ts

prod-pasta-evidence: ## Before bootstrap.sh on a CI runner: passt's version, pasta's AppArmor signal rules, the reproducer and the kernel's denials; never fails
	sh scripts/pasta-check.sh evidence

prod-pasta-check: ## After the install: every unit stopped and started, the reproducer as ronote between, and no AppArmor denial (sudo)
	sudo sh scripts/pasta-check.sh check

# The ARM job's reports, as Markdown: each into bin/reports, then printed to
# the log, keeping the command's exit; make ci-summary puts them in the
# job's summary, and the job uploads the directory. report FILE,COMMAND.
REPORTS = bin/reports
ARM_REPORTS = arm64-images.md docling-curve.md worst-pdf.md memory.md ollama-settings.md
report = mkdir -p $(REPORTS); status=0; $(2) >$(REPORTS)/$(1) || status=$$?; cat $(REPORTS)/$(1); exit $$status

# The worst PDF the units accept (apps/web/scripts/worst-pdf.ts), printed
# here, never committed: as many pages as the cap, docling.MaxPages.
WORST_PDF = bin/worst.pdf
WORST_PAGES := $(shell sed -n 's/^const MaxPages = //p' internal/docling/client.go)

prod-worst-pdf: | bin ## Print the worst PDF the units accept ($(WORST_PAGES) pages of code and tables) and import it through Caddy, timing each step and searching meanwhile, into bin/reports/worst-pdf.md and the log (make prod-real first)
	rm -f $(REPORTS)/worst-pdf.md && mkdir -p $(REPORTS)
	$(WEB) exec node scripts/worst-pdf.ts $(CURDIR)/$(WORST_PDF) $(WORST_PAGES) ../..
	status=0; RONOTE_SMOKE_SPEC=worst RONOTE_WORST_PDF=$(CURDIR)/$(WORST_PDF) RONOTE_WORST_PAGES=$(WORST_PAGES) RONOTE_WORST_REPORT=$(CURDIR)/$(REPORTS)/worst-pdf.md \
		RONOTE_SMOKE_CA=$(CURDIR)/$(SMOKE_CA) $(WEB) exec playwright test --config playwright.smoke.config.ts || status=$$?; \
		cat $(REPORTS)/worst-pdf.md 2>/dev/null; exit $$status

# The page counts docling is timed at, each a PDF printed as the worst one
# is, up to the cap.
CURVE_PAGES = 25 50 $(WORST_PAGES)

prod-docling-curve: | bin ## Docling's time, anon and swap for PDFs of $(CURVE_PAGES) pages, each converted once by the worker's own client with no retries, into bin/reports/docling-curve.md and the log (make prod-real first; sudo)
	for n in $(CURVE_PAGES); do $(WEB) exec node scripts/worst-pdf.ts $(CURDIR)/bin/curve-$$n.pdf $$n ../.. || exit 1; done
	@$(call report,docling-curve.md,sudo sh scripts/real-images.sh curve $(foreach n,$(CURVE_PAGES),$(n) bin/curve-$(n).pdf))

prod-memory-sampler: | bin ## Start sampling the memory of ronote's containers every second into bin/memory-samples, in the background, until make prod-memory (sudo)
	sudo rm -f bin/memory-samples bin/memory-samples.names bin/memory-samples.pid
	sudo sh -c 'nohup sh scripts/real-images.sh sample bin/memory-samples >/dev/null 2>&1 & echo $$! > bin/memory-samples.pid'

prod-memory: | bin ## Stop the sampler; each container's peak memory, anon and file against its limit, then podman system df, into bin/reports/memory.md and the log; fails on an OOM kill or a restart (sudo)
	@$(call report,memory.md,sudo sh scripts/real-images.sh memory bin/memory-samples)

prod-ollama-settings: | bin ## The settings Ollama logged as it started and loaded the model, into bin/reports/ollama-settings.md and the log (sudo)
	@$(call report,ollama-settings.md,sudo sh scripts/real-images.sh ollama)

prod-arm64-images: | bin ## Fail unless every digest-pinned image is a multi-arch index with linux/amd64 and linux/arm64, into bin/reports/arm64-images.md and the log
	@$(call report,arm64-images.md,sh scripts/real-images.sh arm64)

ci-summary: | bin ## On CI: append the reports in bin/reports to the job's summary, in the job's order
	@for f in $(ARM_REPORTS); do [ ! -f $(REPORTS)/$$f ] || { cat $(REPORTS)/$$f; echo; }; done >>"$${GITHUB_STEP_SUMMARY:?not on CI}"

prod-local: prod-config | bin ## The same in the podman machine's VM, then the smoke suite from this Mac
	INSTALL_ENV='$(CI_INSTALL_ENV)' DROPINS='$(CI_FAKES_DROPINS)' sh scripts/prod-local.sh

deploy-host: ## Deploy TAG on the production host over SSH (release only: DEPLOY_HOST, DEPLOY_SSH_KEY, DEPLOY_KNOWN_HOSTS)
	sh scripts/deploy-host.sh "$(TAG)"

prod-journals: | bin ## Save what ronote's units logged since boot, and their containers' states, in bin/prod-journals.log, for a failed CI run; "no units installed" if none was (sudo)
	sudo sh scripts/prod-journals.sh bin/prod-journals.log

backup-now: ## Back up the database and the PDFs now, as the timers do, on a host install.sh set up (sudo)
	sudo sh deploy/run-now.sh ronote-backup@db.service ronote-backup@pdfs.service

restore-drill: ## Run the restore drill now, as its weekly timer does, on a host install.sh set up (sudo)
	sudo sh deploy/run-now.sh ronote-restore-drill.service

watch-now: ## Run the health watch now, as its 5-minute timer does, on a host install.sh set up (sudo); its alerts go to ntfy, or say they would
	sudo sh deploy/run-now.sh ronote-watch.service

prod-smoke: | bin ## Run the smoke suite through Caddy against installed units (make prod-ci first)
	RONOTE_SMOKE_CA=$(CURDIR)/$(SMOKE_CA) $(WEB) exec playwright test --config playwright.smoke.config.ts

# check-config on deploy/ronote.env.example, with stand-ins for its secrets,
# --offline since its blob store is an example: it must pass, and fail with
# the runner on the app's site.
EXAMPLE_SECRETS = -e RONOTE_DATABASE_URL=postgres://ronote:example@ronote-postgres:5432/ronote \
	-e RONOTE_GITHUB_CLIENT_SECRET=example -e RONOTE_ANTHROPIC_API_KEY=example \
	-e RONOTE_BLOB_ACCESS_KEY=example -e RONOTE_BLOB_SECRET_KEY=example \
	-e RONOTE_DOCLING_API_KEY=example -e RONOTE_PDFTEXT_API_KEY=example
CHECK_CONFIG = podman run --rm --env-file deploy/ronote.env.example $(EXAMPLE_SECRETS)

units-check: ## Convert the Quadlet units with Quadlet's dry run (skipped where Quadlet isn't installed)
	sh deploy/units-check.sh

prod-check-config: ## Check the example production config with the api image; a same-site runner must fail
	$(CHECK_CONFIG) ghcr.io/rokumate/ronote-api:$(IMAGE_TAG) check-config --offline
	! $(CHECK_CONFIG) -e RONOTE_RUNNER_URL=https://run.ronote.example.me ghcr.io/rokumate/ronote-api:$(IMAGE_TAG) check-config --offline

.PHONY: help setup hooks-test check-commits tools check test test-go test-perf test-web run-api run-worker run-idp run-llm run-docling pdf-fixtures pdf-record extract lint lint-go lint-web fmt
.PHONY: doctor stop dev-up dev-up-docling dev-down e2e-stack-up e2e-stack-down dev-logs db-shell migrate-up migrate-down migrate-status migrate-new river-migration sqlc
.PHONY: generate check-generated check-go-version ci-cache-keys
.PHONY: test-integration eval eval-record eval-review eval-chat test-perf-search test-live test-clean web-install web-deps web-browsers check-node-version
.PHONY: web-dev web-build runner-pages runner-deploy deploy-host web-e2e images ci-limit-cpus prod-real prod-real-smoke prod-docling-curve prod-worst-pdf prod-memory-sampler prod-ollama-settings ci-summary prod-pasta-evidence prod-pasta-check prod-memory prod-arm64-images ci-clear-environment prod-journals backup-now restore-drill watch-now units-check prod-config prod-ci prod-local prod-smoke prod-check-config
