# Commands

Every make target and what it runs, in detail. AGENTS.md keeps one line each.

| Command | What it does |
|---|---|
| `make help` | list targets (default target) |
| `make setup` | point git at `.githooks`, make the hook executable, set `pull.rebase true`; run once per clone |
| `make hooks-test` | test the commit message hook (normal and strict mode), `scripts/check-commits.sh`, the clock check of `scripts/container.sh` (against fake `podman`, `docker` and `date`), and `scripts/run.sh` with `scripts/stop.sh` |
| `make check-commits RANGE=main..HEAD` | check every commit message in `RANGE` in strict mode; in CI the event sets the range (`scripts/commit-range.sh`). `scripts/check-commits.sh` exits 0 when all pass, 1 when any is bad (report on stderr), 2 on a usage or internal error |
| `make tools` | install the pinned golangci-lint into `./bin` |
| `make check` | `lint`, `test`, `test-perf` and `hooks-test`: what CI's fast jobs run. Run it before pushing |
| `make lint` | `lint-go` and `lint-web` |
| `make lint-go` | golangci-lint (`.golangci.yml`, includes `integration`, `live` and `perf` files), then `go tool deadcode -test -tags integration,live,perf ./...`, which must print nothing outside `internal/httpapi/gen/` (generated helpers may go unused), then `go mod tidy -diff`, then `go tool actionlint` on `.github/workflows` |
| `make lint-web` | `biome check --error-on-warnings`, `tsc -b`, `knip`, all with zero findings |
| `make fmt` | format Go code with gofumpt and goimports |
| `make test` | `test-go` and `test-web` |
| `make test-go` | Go unit tests with `-race` |
| `make test-perf` | timing tests alone: files tagged `perf`, tests named `TestPerf...`, run with `go test -tags perf -p 1` (one package at a time) and without the race detector. A timing test checks growth (time for 2n against n), never an absolute duration, and lives only here: `make test` and `make test-integration` never build it |
| `make test-web` | web unit tests (Vitest, jsdom) |
| `make test-integration` | unit and integration tests (`integration` build tag, testcontainers); fails if a test container is left behind |
| `make test-live` | extract the real pages listed in `internal/extract/live_test.go` over the network and print a table: adapter, blocks, code found vs an independent count, verbatim check, warnings, junk; then `apps/web/live/rag.test.ts` extracts the RAG tutorial and fails if any of its code blocks would offer Run. Needs the network, so it never runs in CI |
| `make test-clean` | remove leftover testcontainers containers |
| `make web-install` | `web-deps` and `web-browsers` |
| `make web-deps` | `pnpm install --frozen-lockfile` |
| `make web-browsers` | Playwright's Chromium; `WITH_DEPS=1` also installs its system packages (CI) |
| `make check-node-version` | print the Node version and fail unless it is `.nvmrc`'s |
| `make web-dev` | Vite dev server on `127.0.0.1:5173`; the styleguide is at `/_styleguide`. It proxies `/api` to the API at `RONOTE_API_ORIGIN` (default `http://127.0.0.1:8080`), so run `make run-api` and `make run-worker` too. It also starts the runner, the origin code from documents runs on (`vite.runner.config.ts`, `localhost:5174`, docs/adr/0008-in-browser-code-execution.md) |
| `make web-build` | production build into `apps/web/dist`, and the runner's into `apps/web/dist-runner`; fails if styleguide code ends up in it or a chunk group passes its gzip budget in `apps/web/scripts/budget.ts`, read from Vite's manifest: initial JS 150 kB, the document route, the highlighter core, the highlight and note controls (a chunk loaded once the editor is up), running code (classifier, controls, output, checked examples, session status and runner client, a chunk loaded with a document's code blocks) and CSS each their own; the runner's own files, its JavaScript worker with acorn included, are not budgeted; Shiki's language chunks are listed, not budgeted |
| `make web-e2e` | Playwright end-to-end and axe tests in Chromium against the full stack: it recreates and migrates the `ronote_e2e` database (`E2E_DATABASE_URL`; locally in the dev stack's Postgres, so run `make dev-up` first), builds the API and worker, and Playwright starts them (API on 18080, worker), the fixture site (`apps/web/e2e/fixture-site.ts` on `localhost:18090`, serving `testdata/golden` pages and the linked pages in `apps/web/e2e/fixtures`), the runner on `localhost:5181` and Vite on 5180. Kept out of `make test` |
| `make run-worker` | run the import worker (River queue `imports`); `RONOTE_ENV` defaults to `dev` |
| `make run-api` | run the API on `127.0.0.1:8080`; `RONOTE_ENV` defaults to `dev` |
| `make extract url=... [format=text]` | fetch one page with the production fetcher rules and print its IR as JSON, or as readable text |
| `make dev` (P0x) | run API, worker and web locally |
| `make doctor` | check the local setup: the container VM's clock is within 2s of the host's (podman machine, or Docker's VM). On failure it prints the offset and the fix, such as `podman machine ssh sudo chronyc makestep` |
| `make dev-up` | start the dev stack (`deploy/compose.dev.yml`) and wait until healthy; creates `.env.dev` if missing |
| `make stop` | end what `make run-api`, `make run-worker` and `make web-dev` started, with everything they started (such as the binary `go run` builds): each runs through `scripts/run.sh`, which records its process group in `.run/`; `scripts/stop.sh` sends TERM to each group, then KILL after 5s |
| `make dev-down` | `make stop`, then stop the dev stack; the data volume is kept |
| `make dev-logs` | follow the dev stack logs |
| `make db-shell` | psql on the dev database |
| `make build` (P0x) | build the Go binaries and the web bundle |
| `make migrate-up` / `migrate-down` / `migrate-status` | apply, roll back the latest, or list goose migrations against `RONOTE_DATABASE_URL` |
| `make migrate-new name=...` | create the next sequential SQL migration in `migrations/` |
| `make river-migration first=N last=M` | add a migration holding River's own migrations N to M, from the River version go.mod pins. Goose owns all schema, River's tables included; after a River upgrade, `make test-integration` names the versions to add |
| `make sqlc` | regenerate `internal/store/sqlcdb` from `migrations/` and `internal/store/queries/`; commit the result |
| `make generate` | regenerate everything generated: sqlc output, then `internal/httpapi/gen` (oapi-codegen) and `apps/web/src/api/schema.d.ts` (openapi-typescript) from `api/openapi.yaml`; commit the result. Needs Node as well as Go |
| `make check-generated` | `make generate`, then fail if the output differs from what is committed |
| `make check-go-version` | print the Go toolchain and fail unless it is go.mod's `toolchain` |

Targets marked (P0x) do not exist yet. The phase that adds a target removes its tag.

Web targets, `make lint` and `make test` check that `node --version` matches `.nvmrc` first; with nvm, `nvm install && nvm use` picks it up.

Go CLIs are pinned as `tool` directives in go.mod (`go get -tool`) and run with `go tool <name>`: deadcode, goose, sqlc, river, oapi-codegen. golangci-lint is the exception: `make tools` installs the version pinned in the Makefile into `./bin`.

The API is spec-first: change `api/openapi.yaml`, then `make generate`. Never hand-write a type the spec defines: Go uses `internal/httpapi/gen`, TypeScript imports from `apps/web/src/api/schema.d.ts`, and a Vitest test fails on a TypeScript copy of a spec type. Error codes are enums in the spec (`ErrorCode`, `ImportErrorCode`), so Go uses the generated constants. The spec's IR schema maps onto `internal/ir` (`x-go-type`), and `TestIRSchemaMatchesGoldens` keeps the two in step.
