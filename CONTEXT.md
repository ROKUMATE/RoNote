# CONTEXT.md

The project's state, in at most 80 lines and 6 KB (`make lint` checks). Rewrite it at the end of each phase; never append logs. Why things are as they are lives in `docs/adr/` and `docs/decisions.md`; what is left open, in `docs/open-questions.md`; the architecture, in `SYSTEM-DESIGN.md`.

## Current milestone

P19b done: operations for one real host (ADR 0015), proven in CI on 2 CPUs, as on that host, with no host yet. Going live is the operator's, by `docs/runbook.md`.
- **Host:** Oracle's A1, 2 OCPUs and 12 GB (the whole Always Free allowance), Ubuntu 26.04 aarch64. `deploy/bootstrap.sh` sets it up:
  - Podman, with pasta's AppArmor rule (`deploy/pasta-apparmor.sh`, upstream's unmerged patch);
  - a 2 GiB swap file;
  - SSH by key alone, and 80 and 443 open;
  - the deploy user;
  - then `install.sh`.
- **Deploys:** a `v*` tag builds the images for amd64 and arm64 and waits for approval in `production`. It then deploys the runner to Cloudflare Pages, and `ronote-deploy` pins the images by digest, migrates, checks health, and rolls back by itself on failure.
- **Data:** PDFs in Oracle Object Storage. restic backs up to R2, nightly (the database) and weekly (the PDFs), with a weekly restore drill.
- **Alerts:** ntfy, for a disk over 80%, a unit down for 5 minutes, the certificate within 14 days, the chat spend cap, and failed backups, drills or deploys.
- **Measured limits:**
  - PDFs at most 100 pages, refused after pdftext's inspection with the page count shown. Docling runs at about 1.87 s a page on arm64 (ADR 0013, ARM run 45578157125).
  - Docling's timeout is final (`pdf_conversion_timeout`), not retried.
  - Ollama is pinned to a 4096-token context and one model kept loaded, with anon 1.1 of its 2 GiB. It indexes a chunk at a time (about 1.9 s each on arm64), one job at a time, caching each chunk.
  - Searches embed on a second Ollama, `ronote-ollama-query` (1.75 GB, its model kept resident): the same model from the first's volume read-only, a 2048-token context, so a query never waits behind indexing. Until both serve one digest, and on a mismatch, which alerts, search is by words alone. Embedding is Ollama's native `/api/embed` with truncate off (ADR 0011).
  - CPU weights, not limits: caddy, the api, the query Ollama and Postgres 1000; docling, the indexing Ollama, the worker, pdftext and the backups 50. Threads: query 2, indexing 1, docling 2; the api loads the query model, never a search (ADR 0014).
- **Proof:**
  - The smoke job, on `ubuntu-26.04` and `-arm`, bootstraps the runner and installs the units with the fakes. It also proves the AppArmor rule (every unit stopped and started) and the swap file.
  - The weekly `arm-real` job runs the real docling and Ollama on 2 CPUs (`make ci-limit-cpus`):
    - the curve at 25, 50 and 100 pages, and the worst PDF at the cap, with searches meanwhile: none degraded, the slowest under 1.5 s, converting within 270 s;
    - a backup and the restore drill;
    - each container's peak, anon, file, swap and CPU weight.

    Its reports go to the log and the artifact `arm-real-reports`.
- **Tooling:**
  - Renovate: weekly grouped PRs, digests pinned, one-line commits.
  - `make web-e2e` on its own compose project, `ronote-e2e`.
  - Tests never use Go's shared HTTP transport (`internal/httpguard`); checks that every target makes `bin/` first, and every named `docs/*.md` exists.

## What exists

- **Go** (`cmd/api`, `cmd/worker`, dev tools in `cmd/`, `internal/*`): a spec-first API (`api/openapi.yaml`) behind GitHub sign-in; hybrid search over each library and chat over it; an import pipeline on the jobs port for pages (fetch with SSRF guard and robots.txt, extract to IR with framework adapters) and PDFs (docling and pdftext), documents per owner. Event streams come from Postgres LISTEN/NOTIFY; editing keeps source blocks locked; highlights and notes. `worker convert-check` converts one PDF with the import's client, for measuring.
- **Web** (`apps/web`):
  - the library, and an import dialog for pages and PDFs with live progress;
  - the document reader and editor (Tiptap), with same-domain links that import on click, and PDF page links;
  - code highlighting and language choice, highlights and notes, and code that runs in the browser;
  - the search palette and the chat panel.
- **Runner** (`apps/web/runner`): its own origin (Cloudflare Pages in production), Pyodide and JavaScript workers, a versioned message protocol.
- **Quality:** golden fixtures and live checks for the extractor, recorded docling fixtures with goldens and the PDF verbatim report, integration tests on testcontainers, Playwright and axe end to end against the fakes, CI on every push and pull request, the search eval in CI.
- **Dev stack:** `make dev-up` (Postgres, SeaweedFS), `make dev-up-docling` (the real docling and pdftext); `make doctor` checks them.
- **Production:** `deploy/` holds the images, the Quadlet units, the Caddyfile, `bootstrap.sh`, `install.sh`, `ronote-deploy`, the backups and the alerts (ADR 0014, ADR 0015).
- **ADRs:**
  - 0001 stack, 0002 verbatim source, 0003 commits;
  - 0004 IR and links, 0005 import pipeline, 0006 editor and DocContent;
  - 0007 annotations, 0008 in-browser code execution, 0009 the jobs port;
  - 0010 sign-in and sessions, 0011 retrieval, 0012 chat, 0013 PDF import;
  - 0014 production topology, 0015 operations.

## Next step

Go live by `docs/runbook.md`.
- **Before the instance:** the domain, Cloudflare, ntfy, the OAuth app, chat, and the `production` environment.
- **The instance:** create it (100 GB boot volume), bootstrap it, and tag the first release.
- **First-release checks:**
  - a 100-page PDF through `worker convert-check`, against CI's 187 s and 2.85 GiB;
  - Let's Encrypt, real client IPs, and the runner's headers;
  - the first backup and drill.
- **Then:** install the Renovate app on this repo.
- **Still open:** whether the ghcr.io packages are public, and the model refusing in text instead of calling its tool (docs/open-questions.md).

## Quarantined tests

- #13, "going back returns to the same place" in `apps/web/e2e/links.spec.ts`.
