# Repo layout

Where things live, in full. AGENTS.md keeps the short version.

| Path | Holds |
|---|---|
| `cmd/api/` | HTTP API server |
| `cmd/worker/` | background job runner |
| `cmd/extract/` | dev tool: fetch one page and print its IR (`make extract`) |
| `internal/<pkg>/` | Go packages, one concern each |
| `migrations/` | Postgres schema migrations |
| `apps/web/` | React + TypeScript + Tailwind v4 frontend |
| `apps/web/runner/` | the runner origin's page, and its Python and JavaScript workers (with the cell runner, the JavaScript evaluator and console formatting), where code from documents runs (docs/adr/0008-in-browser-code-execution.md); `apps/web/src/runner/protocol.ts` is the message protocol both sides parse with, and `limits.ts` the limits every runtime keeps |
| `deploy/` | deployment config |
| `scripts/` | repo scripts |
| `internal/ir/` | the document model every importer produces (docs/adr/0004-ir-and-links.md) |
| `internal/doccontent/` | a user's document: IR blocks marked source or user, and the rules a save must keep (docs/adr/0006-editor-and-doc-content.md) |
| `internal/annotation/` | a user's highlights and notes: selectors into one block's plain text, in UTF-16 code units, and the rules a write must keep (docs/adr/0007-annotations-and-anchoring.md) |
| `internal/importer/` | the import pipeline: the River job that fetches, extracts and stores a document, and the service the API queues imports with |
| `internal/events/` | import status changes from Postgres (LISTEN import_status, one connection per API process) to the API's event streams |
| `internal/canon/` | canonical URLs: what sources and imports are keyed by, and the same-host rule for a page's canonical link |
| `internal/fetch/` | the only way to fetch a user-supplied URL: SSRF guard, limits, robots.txt, rate limit |
| `internal/extract/` | HTML to IR extractor: generic rules, refined by a framework adapter when one matches |
| `internal/extract/adapter/` | docs-framework adapters as configuration: detection, content root, chrome, callouts, tabs and code rules |
| `testdata/golden/` | extractor fixtures: `input.html`, `meta.json`, `expected.json`; third-party excerpts are listed in `SOURCES.md` |
| `api/` | `openapi.yaml`, the API contract that Go and TypeScript types are generated from, and the oapi-codegen config |
| `internal/httpapi/gen/` | Go server code generated from `api/openapi.yaml`; never edit it |
| `apps/web/src/api/` | the web app's API layer; `schema.d.ts` is generated from `api/openapi.yaml`, never edit it |
| `tools/openapi-ts/` | the pnpm package that runs openapi-typescript, kept apart because it needs TypeScript 5 and the app uses TypeScript 7 |
| `docs/adr/` | architecture decision records |
| `.githooks/` | git hooks, installed by `make setup` |

Create a directory when its first file needs it, not before.
