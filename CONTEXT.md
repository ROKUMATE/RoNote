# CONTEXT.md

The project's state, in at most 80 lines and 6 KB (`make lint` checks). Rewrite it at the end of each phase; never append logs. Why things are as they are lives in `docs/adr/` and `docs/decisions.md`; what is left open, in `docs/open-questions.md`; the architecture, in `SYSTEM-DESIGN.md`.

## Current milestone

P19a, the production topology (ADR 0014), proven in CI, with no real server yet.
- **One host, rootless Podman, Quadlet** (`deploy/quadlet/`): caddy, api, worker, a one-shot migrate, postgres, ollama, docling, pdftext, seaweedfs, under a lingering `ronote` user. Only Caddy publishes ports (80, 443); data and converters are internal networks. Every unit is read-only, unprivileged, limited and health-checked (`deploy/units_test.go`). Secrets are podman secrets; `deploy/install.sh` makes them and installs everything.
- **Caddy** (`deploy/caddy/Caddyfile`, baked into the web image) serves the app and the runner on two domains from `ronote.env`, each with its own headers. Only the JavaScript worker's script gets `'unsafe-eval'`; Pyodide is precompressed under a versioned path, cached as immutable.
- **Images** (`deploy/images/`): api 23.9 MB and worker 18.4 MB (distroless), web 94.3 MB (Caddy and the static files). A `v*` tag pushes amd64 and arm64 to ghcr.io with build provenance. `/healthz` shows the version.
- **One build for any domains.** The app and the runner read their origins from meta tags that Caddy fills (`RONOTE_PUBLIC_URL`, `RONOTE_RUNNER_URL`). `boot.js` replaces the inline script and ProseMirror's styles are a stylesheet, so the app's CSP allows no inline code.
- **App:**
  - `RONOTE_TRUSTED_PROXIES` for `X-Forwarded-For`;
  - `RONOTE_CHAT_DAILY_USD` caps chat for all users per UTC day (`chat_spend_cap_reached`);
  - `api|worker check-config` (a same-site runner fails);
  - `api migrate`, and a refusal to start on a newer schema;
  - markers are counted only in unsourced replies.
- **Proof:** the CI job `smoke` (`make prod-ci`, `prod-check-config`, `prod-smoke`) installs the real units with the fakes by drop-ins, and runs Playwright through Caddy at `ronote.test` and `ronote-run.test`. `make prod-local` does the same in the podman VM; it passed 8 of 8, and giving the runner the app's CSP failed it.
- **Before this, P18.3:** chat says "not in your library" with a tool, and an answer citing nothing is `unsourced`.

## What exists

- **Go** (`cmd/api`, `cmd/worker`, dev tools in `cmd/`, `internal/*`): a spec-first API (`api/openapi.yaml`) behind GitHub sign-in; hybrid search over each library and chat over it; an import pipeline on the jobs port for pages (fetch with SSRF guard and robots.txt, extract to IR with framework adapters) and PDFs (docling and pdftext), documents per owner. Event streams come from Postgres LISTEN/NOTIFY; editing keeps source blocks locked; highlights and notes.
- **Web** (`apps/web`):
  - the library, and an import dialog for pages and PDFs with live progress;
  - the document reader and editor (Tiptap), with same-domain links that import on click, and PDF page links;
  - code highlighting and language choice, highlights and notes, and code that runs in the browser;
  - the search palette and the chat panel.
- **Runner** (`apps/web/runner`): its own origin, Pyodide and JavaScript workers, a versioned message protocol.
- **Quality:** golden fixtures and live checks for the extractor, recorded docling fixtures with goldens and the PDF verbatim report, integration tests on testcontainers (Postgres, SeaweedFS), Playwright and axe end to end against fake sign-in, embedder, model and docling, CI on every push and pull request; the search eval in CI.
- **Dev stack:** `make dev-up` (Postgres, SeaweedFS with its bucket), `make dev-up-docling` (the real docling and pdftext); `make doctor` checks them.
- **Production:** `deploy/` holds the images, the Quadlet units, the Caddyfile and `install.sh`. CI's `smoke` job and `make prod-local` run them with the fakes (ADR 0014).
- **ADRs:**
  - 0001 stack, 0002 verbatim source, 0003 commits;
  - 0004 IR and links, 0005 import pipeline, 0006 editor and DocContent;
  - 0007 annotations, 0008 in-browser code execution, 0009 the jobs port;
  - 0010 sign-in and sessions, 0011 retrieval, 0012 chat, 0013 PDF import, 0014 production topology.

## Next step

P19b: a real host. Push to see the smoke job on GitHub's runner, then install from a release tag with real domains, GitHub's OAuth app and Let's Encrypt; prove docling's limits and Ollama's model there; set up backups; turn chat on by env. Still open: the model refusing in text instead of calling the tool (docs/open-questions.md).

## Quarantined tests

- #13, "going back returns to the same place" in `apps/web/e2e/links.spec.ts`.
