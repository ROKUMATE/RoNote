# Local containers and running locally

The local stack: containers, the dev environment, and the processes dev runs.

## Local containers

- macOS uses podman machine (rootless); Linux and CI use Docker. `scripts/container.sh` picks whichever answers.
- `podman compose` needs a compose provider: `brew install docker-compose` (or podman-compose). `make dev-up` fails with a clear message when neither `podman compose` nor `docker compose` works.
- After the Mac sleeps, podman machine's clock can lag the host's for minutes. Postgres then writes times on the VM's clock while the app uses the host's; River copes since P12 (ADR 0005), but a test that compares the two still fails. Run `make doctor` when integration tests fail oddly.
- `.env.example` lists every variable. Copy it to `.env.dev` (gitignored); the Makefile loads it, its values override the shell environment, and `make VAR=value` still wins.

## Running locally

Dev runs two processes, each in its own terminal, after `make dev-up` and `make migrate-up`:
- `make run-api` serves the API. Set `RONOTE_DEV_USER_EMAIL` (in `.env.dev`, or `make run-api RONOTE_DEV_USER_EMAIL=...`), or every `/api/v1` route answers 401.
- `make run-worker` runs imports. Without it, imports stay `queued`.

docs/adr/0005-import-pipeline.md describes the pipeline both serve.
