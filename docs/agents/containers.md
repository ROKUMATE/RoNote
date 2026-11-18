# Local containers and running locally

The local stack: containers, the dev environment, and the processes dev runs.

## Local containers

- macOS uses podman machine (rootless); Linux and CI use Docker. `scripts/container.sh` picks whichever answers.
- `podman compose` needs a compose provider: `brew install docker-compose` (or podman-compose). `make dev-up` fails with a clear message when neither `podman compose` nor `docker compose` works.
- After the Mac sleeps, podman machine's clock can lag the host's for minutes. Postgres then writes times on the VM's clock while the app uses the host's; River copes since P12 (ADR 0005), but a test that compares the two still fails. Run `make doctor` when integration tests fail oddly.
- `make dev-up` runs Postgres and SeaweedFS, the S3-compatible store uploaded PDFs go to (`127.0.0.1:8333`); SeaweedFS's healthcheck makes the `ronote` bucket, so `--wait` returns once it exists. `make dev-up-docling` adds the real docling and pdftext (`127.0.0.1:5001`, `:5002`): a 5 GB image wanting 6 GB of memory (`podman machine set --memory 8192`), on an internal network with no egress that only `converters-gate`, a socat forwarder, reaches (docs/adr/0013-pdf-import.md).
- `.env.example` lists every variable. Copy it to `.env.dev` (gitignored); the Makefile loads it, its values override the shell environment, and `make VAR=value` still wins.

## Running locally

Dev runs two processes, each in its own terminal, after `make dev-up` and `make migrate-up`:
- `make run-api` serves the API. Every `/api/v1` route but sign-in answers 401 without a session: sign in at `/login` through the fake GitHub (`cmd/fakeidp`), which `make web-dev` starts (or `make run-idp` alone). `.env.example`'s provider URLs point at it, and its users (`deploy/fakeidp/dev-users.json`) include `dev@ronote.local`, so documents from before sign-in stay reachable (docs/adr/0010-sign-in-and-sessions.md).
- `make run-worker` runs imports. Without it, imports stay `queued`.
- Chat answers from the fake model (`cmd/fakellm`), which `make web-dev` starts (or `make run-llm` alone), as `.env.example`'s `RONOTE_CHAT_URL` says. For the real model, set `RONOTE_CHAT_URL=https://api.anthropic.com` and `RONOTE_ANTHROPIC_API_KEY` in `.env.dev`; each question then costs a fraction of a cent (docs/adr/0012-chat.md).

- PDFs convert through the fake docling and pdftext (`cmd/fakedocling`), which `make web-dev` starts (or `make run-docling` alone): it replays the answers recorded for the fixtures in `testdata/pdf` and answers 503 to any other PDF. For any PDF of your own, stop it and run `make dev-up-docling`; both use the same addresses and keys.

docs/adr/0005-import-pipeline.md describes the pipeline both serve.
