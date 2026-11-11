# CONTEXT.md

The project's state, in at most 80 lines and 6 KB (`make lint` checks). Rewrite it at the end of each phase; never append logs. Why things are as they are lives in `docs/adr/` and `docs/decisions.md`; what is left open, in `docs/open-questions.md`; the architecture, in `SYSTEM-DESIGN.md`.

## Current milestone

P15, sign-in, sessions, quotas and account deletion: done (ADR 0010).
- **Sign-in:**
  - GitHub OAuth with state and PKCE, the same path in every environment;
  - dev and test point it at `cmd/fakeidp` (`make run-idp`; `web-dev` and `web-e2e` start it), and config refuses anything but github.com in staging and prod;
  - a user is a `(provider, provider_user_id)` identity; a verified email links an existing user, so `dev@ronote.local`'s documents stay reachable.
- **Sessions:**
  - in Postgres, SHA-256 of the token only, 30 days idle and 90 in all;
  - the cookie is host-only and HttpOnly, `Secure` and `__Host-` in staging and prod;
  - sign out, sign out everywhere, and account deletion that cascades and keeps shared sources.
- **CSRF:** `http.CrossOriginProtection` on every `/api` route.
- **Limits:**
  - quotas of 60 imports a day and 500 documents, in Postgres under a per-user lock;
  - 120 requests a minute per user and 20 sign-ins a minute per IP, in memory per process;
  - a body limit on every route.
- **Web:** `/login`, with a sentence per error; any 401, and a refused event stream, go there once and come back; the account menu and the delete dialog sit in the sidebar footer.
- **Tests:**
  - each e2e worker signs in as its own user; `auth.spec.ts` covers the flows;
  - three deliberate breaks were each caught: the state check, `expires_at`, and `//host` in `return_to`.
- **Checks:** `make check` exits 0; `make test-integration` exits 0; `make web-e2e` passed 107, skipped 1 (quarantined).
- **#13** with `fixme` lifted: 9 of 10 runs passed locally; one landed 112px off. It stays quarantined.

## What exists

- **Go** (`cmd/api`, `cmd/worker`, `cmd/fakeidp`, `internal/*`): a spec-first API (`api/openapi.yaml`) behind GitHub sign-in, and an import pipeline on the jobs port (fetch with SSRF guard and robots.txt, extract to IR with framework adapters, documents per owner). Event streams come from Postgres LISTEN/NOTIFY; editing keeps source blocks locked; highlights and notes.
- **Web** (`apps/web`):
  - the library, and an import dialog with live progress;
  - the document reader and editor (Tiptap), with same-domain links that import on click;
  - code highlighting and language choice, highlights and notes, and code that runs in the browser.
- **Runner** (`apps/web/runner`): its own origin, Pyodide and JavaScript workers, a versioned message protocol.
- **Quality:** golden fixtures and live checks for the extractor, integration tests on testcontainers, Playwright and axe end to end, CI on every push and pull request.
- **ADRs:**
  - 0001 stack, 0002 verbatim source, 0003 commits;
  - 0004 IR and links, 0005 import pipeline, 0006 editor and DocContent;
  - 0007 annotations, 0008 in-browser code execution, 0009 the jobs port;
  - 0010 sign-in and sessions.

## Next step

The next phase, per its brief. v1 still lacks printed-PDF import; the runner's deployment is P18, proxy headers P19.

## Quarantined tests

- #13, "going back returns to the same place" in `apps/web/e2e/links.spec.ts`.
