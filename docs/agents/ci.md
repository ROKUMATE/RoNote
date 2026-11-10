# CI

Read before changing `.github/workflows` or a target CI calls.

`.github/workflows/ci.yml` runs on every push to `main` and every pull request.
- Steps only call make targets. Anything CI needs becomes a make target first, so every job can be reproduced locally.
- Actions are pinned to full commit SHAs with the version in a trailing comment. When you bump one, update the SHA and the comment together; `git ls-remote https://github.com/OWNER/REPO 'refs/tags/TAG^{}'` gives the SHA.
- The workflow has `contents: read` permissions, cancels superseded runs for the same ref, and sets a timeout on every job. `make lint-go` runs actionlint on it.
- Every job runs on `ubuntu-26.04`, never `ubuntu-latest`, so the runner image only changes in a reviewed commit. `.github/actionlint.yaml` declares the label until actionlint knows it (rhysd/actionlint#682).

| Job | Runs | Reproduce locally |
|---|---|---|
| commits | `make check-commits` (the strict hook over a PR's base..head, or a push's before..sha; `sha~1..sha` after a force push or a new branch), then `make hooks-test` with dash and GNU tools, then `make lint-docs` | `make check-commits RANGE=main..HEAD`, `make hooks-test`, `make lint-docs` |
| go | `make check-go-version`, `make tools`, `make lint-go`, `make test-go`, `make test-perf`, then Node and `make web-deps` for `make check-generated` | the same targets |
| integration | `make check-go-version`, `make test-integration` on the runner's Docker with Ryuk on | `make test-integration` |
| web | `make check-node-version`, `make web-deps`, `make lint-web`, `make test-web`, `make web-build` (styleguide guard and bundle budget) | the same targets |
| e2e, after web | a Postgres service with an empty `ronote_e2e`, `make check-go-version`, `make check-node-version`, `make web-browsers WITH_DEPS=1`, `make web-e2e` (migrations, API, worker, fixture site, runner, Vite); uploads `apps/web/playwright-report` for 7 days when it fails | `make dev-up`, `make web-install`, `make web-e2e` |

Pull requests merge by rebase only (a repository setting). A merge commit would carry the PR title as a second line, which `check-commits` rejects.

Each job checks the toolchain it uses against its pin: Go against go.mod's `toolchain`, Node against `.nvmrc`. `make check` covers the commits, go and web jobs except `check-generated` and `web-build`. Run it before pushing.
