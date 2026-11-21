# CI

Read before changing `.github/workflows` or a target CI calls.

`.github/workflows/ci.yml` runs on every push to `main` and every pull request.
- Steps only call make targets. Anything CI needs becomes a make target first, so every job can be reproduced locally.
- Actions are pinned to full commit SHAs with the version in a trailing comment. When you bump one, update the SHA and the comment together; `git ls-remote https://github.com/OWNER/REPO 'refs/tags/TAG^{}'` gives the SHA.
- The workflow has `contents: read` permissions, cancels superseded runs for the same ref, and sets a timeout on every job. `make lint-go` runs actionlint on it.
- Every job runs on `ubuntu-26.04` (and the smoke job also on `ubuntu-26.04-arm`), never `ubuntu-latest`, so the runner image only changes in a reviewed commit. `.github/actionlint.yaml` declares the label until actionlint knows it (rhysd/actionlint#682).

| Job | Runs | Reproduce locally |
|---|---|---|
| commits | `make check-commits` (the strict hook over a PR's base..head, or a push's before..sha; `sha~1..sha` after a force push or a new branch), then `make hooks-test` with dash and GNU tools, then `make lint-docs` | `make check-commits RANGE=main..HEAD`, `make hooks-test`, `make lint-docs` |
| go | `make check-go-version`, `make tools`, `make lint-go`, `make test-go`, `make test-perf`, then Node and `make web-deps` for `make check-generated` | the same targets |
| integration | `make check-go-version`, `make test-integration` on the runner's Docker with Ryuk on | `make test-integration` |
| web | `make check-node-version`, `make web-deps`, `make lint-web`, `make test-web`, `make web-build` (styleguide guard and bundle budget) | the same targets |
| e2e, after web | a Postgres service with an empty `ronote_e2e`, the dev stack's SeaweedFS through `docker compose up --wait seaweedfs` (a service container can't take its command), `make check-go-version`, `make check-node-version`, `make web-browsers WITH_DEPS=1`, `make web-e2e` (migrations, API, worker, the fake sign-in, embedder, chat model and docling, fixture site, runner, Vite; no key is set, no test reaches a real model, and docling's image is never pulled); uploads `apps/web/playwright-report` for 7 days when it fails | `make dev-up`, `make web-install`, `make web-e2e` |
| smoke, after web, on `ubuntu-26.04` and `ubuntu-26.04-arm` (a matrix; production is arm64) | `make units-check` (this runner's Quadlet converts every unit), Node, `make web-deps`, `make web-browsers WITH_DEPS=1`, then `make prod-ci` (images, `deploy/install.sh` with the fakes' drop-ins under a rootless `ronote` user, the port check), `make prod-check-config` and `make prod-smoke` through Caddy, then `make backup-now` and `make restore-drill` into a restic repository on the runner, and `make watch-now`, whose alerts only say what they would send; uploads the smoke suite's report for 7 days when it fails | `make prod-local` on a Mac with podman machine, or `make prod-ci prod-check-config prod-smoke` on Linux |

`.github/workflows/release.yml` runs on a `v*` tag: for each of `ronote-api`, `ronote-worker` and `ronote-web` it builds amd64 and arm64 from `deploy/images/` with buildx (Go cross-compiles, so nothing runs emulated), pushes `ghcr.io/rokumate/<image>:<tag>` and attests its build provenance. Its job alone has `packages: write`, `id-token: write` and `attestations: write`. It uses the Docker build actions rather than make targets, since pushing a multi-arch image is what those actions are for; `make images` builds the same Containerfiles for this machine's architecture. Its `deploy` job then runs in the environment `production`, which waits for its required reviewer: `make web-build`, `make runner-pages APP_URL=` the environment's variable `RONOTE_PUBLIC_URL`, `make runner-deploy` with the environment's secrets `CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_ACCOUNT_ID` and its variable `CLOUDFLARE_PAGES_PROJECT`, then `make deploy-host TAG=` the tag, over SSH with the secret `DEPLOY_SSH_KEY` to the variables `DEPLOY_HOST` and `DEPLOY_KNOWN_HOSTS` (docs/adr/0015-operations.md). wrangler is pinned in `deploy/pages`, a package outside the workspace with its own lockfile, which nothing else installs.

Pull requests merge by rebase only (a repository setting). A merge commit would carry the PR title as a second line, which `check-commits` rejects.

Each job checks the toolchain it uses against its pin: Go against go.mod's `toolchain`, Node against `.nvmrc`. `make check` covers the commits, go and web jobs except `check-generated` and `web-build`. Run it before pushing.
