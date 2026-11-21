# 0014: Production topology

Accepted, 2026-11-20. Amended 2026-11-21: Ubuntu 26.04 LTS only; the memory budget is for the 12 GB host; Caddy serves only the app, with the runner on Cloudflare Pages; SeaweedFS runs only in CI and `make prod-local` (ADR 0015).

## Context

From P19 on, RoNote runs somewhere other than a laptop. Earlier ADRs left the deployment open but fixed its constraints: the runner on its own registrable domain with its own headers (ADR 0008), Secure `__Host-` cookies and a client IP that comes from a proxy only once its headers are trusted (ADR 0010), chat that costs money per question (ADR 0012), and docling and pdftext with no way out (ADR 0013). One small host is enough for the users RoNote will have first, and a topology that fits on one host also fits on a laptop and in CI, where it can be proven before a real server exists.

## Decision

- **One Linux host**, Ubuntu 26.04 LTS (Podman 5.0 or later; 24.04's 4.9.3 rejects Quadlet's `Entrypoint=`, and `install.sh` refuses it), arm64 or amd64: Oracle's 2-OCPU, 12 GB arm64 instance in production (ADR 0015).
  - Rootless Podman, under a dedicated `ronote` user with lingering on, so its services start at boot without a login.
  - Services are Quadlet units (`deploy/quadlet/`) in that user's `~/.config/containers/systemd/`; systemd orders, restarts and health-checks them.
  - `net.ipv4.ip_unprivileged_port_start=80`, so Caddy binds 80 and 443 without root.
  - Podman's default network, pasta, keeps a client's source address, so Caddy sees real client IPs.
- **Containers:**

  | Unit | Image | Role |
  |---|---|---|
  | caddy | `ghcr.io/rokumate/ronote-web` | TLS, the app's static files, `/api` to the api; in CI and `make prod-local` also the runner's |
  | api | `ghcr.io/rokumate/ronote-api` | the HTTP API |
  | worker | `ghcr.io/rokumate/ronote-worker` | jobs: imports, PDFs, indexing, blob deletion |
  | migrate | `ghcr.io/rokumate/ronote-api` | `api migrate`, once, before api and worker start |
  | postgres | pgvector, the digest in `deploy/compose.dev.yml` | the database |
  | ollama | Ollama, pinned by digest | embeddings; the model is pulled at install, its digest checked against the pin, and the service then has no egress |
  | docling, pdftext | the docling image of ADR 0013, pinned by digest | PDF conversion, no egress |
  | seaweedfs | SeaweedFS, pinned by digest | the blob store in CI and `make prod-local` only, by drop-in; production uses Oracle Object Storage (ADR 0015) |

- **Networks.** Only Caddy publishes ports: 80 and 443 (and 443/udp, HTTP/3), to its 8080 and 8443, since it runs as a user of its own.
  - `ronote-edge`: caddy and api; a fixed subnet, which is `RONOTE_TRUSTED_PROXIES`.
  - `ronote-egress`: api (Anthropic, GitHub) and worker (fetching pages).
  - `ronote-data`, internal: api, worker, migrate, postgres, seaweedfs, ollama.
  - `ronote-converters`, internal: worker, docling, pdftext.
- **Every container** runs with a read-only root (writable paths as tmpfs or volumes), `no-new-privileges`, all capabilities dropped but those its entrypoint needs, memory and CPU limits, and a health check; `deploy/units_test.go` checks it. The exceptions say why in their unit: postgres keeps `CHOWN`, `DAC_OVERRIDE`, `FOWNER`, `SETGID` and `SETUID` and seaweedfs `CHOWN`, `SETGID` and `SETUID`, because their entrypoints make their directories as root and then become their own user; the one-shot migrate has no health check. The api's and worker's checks are their own binaries (`healthcheck`), run without a shell, which distroless has none of; the worker, serving no HTTP, touches a file while its database answers.
- **Memory on 12 GB:** caddy 128 MB, api 512 MB, worker 512 MB, postgres 2 GB, ollama 2 GB, docling 4 GB with one conversion worker, pdftext 512 MB; the backup jobs run briefly at night, and about 2.3 GB is left for the host. No unit asks for more than the host's 2 CPUs. In CI, seaweedfs gets 384 MB.
- **Secrets are podman secrets**, created at install and given to units with `Secret=…,type=env`: the GitHub client secret, the Anthropic key, the docling, pdftext and blob keys, and the database password. No secret, and no env file holding one, is in the repo.
- **Two domains, as config:** `RONOTE_PUBLIC_URL` (the app) and `RONOTE_RUNNER_URL` (the runner), both https. They must be different registrable domains (ADR 0008); config compares their eTLD+1 by the public suffix list and refuses a same-site pair.
- **One image serves any deployment.** The app and the runner read both URLs at run time, from meta tags in their `index.html` (`ronote-runner-url`, `ronote-app-url`) that Caddy fills from its environment with `templates` (Vite does the same in dev). Nothing domain-specific is built into the JavaScript.
- **Images** (`deploy/images/`):
  - api and worker: static Go binaries (no cgo), built multi-stage and run on distroless `static-debian13:nonroot`, without shells or dev tools;
  - web: Caddy on Alpine, with the built app and runner, Pyodide precompressed;
  - bases pinned by digest; amd64 and arm64;
  - a `v*` tag builds and pushes them to `ghcr.io/rokumate/ronote-*` with build provenance attached (`actions/attest-build-provenance`);
  - the version is stamped into the binaries and shown by `/healthz`.
- **Caddy.**
  - App domain:
    - the static app, `/assets/*` cached for a year as immutable and `index.html` never;
    - `/api` to the api, unbuffered, for the import events and chat streams;
    - request bodies bounded per route at the API's limits (ADR 0010, ADR 0013).
  - App headers: HSTS, a CSP that allows only its own scripts, styles, fonts and connections (so preferences load from `boot.js` rather than an inline script, ProseMirror's base styles are a stylesheet rather than an injected element, and fonts stay files rather than `data:` URIs), framing only of the runner, and images from any https site (imported pages show their own), `nosniff`, `Referrer-Policy`, `Permissions-Policy`, COOP `same-origin` and COEP `credentialless` (ADR 0008). The API's own headers, such as `/original`'s sandbox, are kept.
  - Runner domain, in CI and `make prod-local` (production's runner is on Cloudflare Pages, ADR 0015): the policies of ADR 0008, from the one definition that also makes `headersFor` and Pages' `_headers`. The JavaScript worker's script alone gets `'unsafe-eval'`. Pyodide is served precompressed under its version's path and cached as immutable.
- **The api and the worker:**
  - trust `X-Forwarded-For` only from `RONOTE_TRUSTED_PROXIES` (CIDRs, none by default);
  - cap chat's spending across all users at `RONOTE_CHAT_DAILY_USD` a UTC day (ADR 0012);
  - check a configuration without starting (`check-config`), never printing a value;
  - refuse to start on a schema newer than they know; migrations run before them, as their own unit.
- **Proof.** A CI job installs these units and this Caddy config on its runner, swapping in the fakes (fakeidp, fakeembed, fakellm, fakedocling) and `RONOTE_ENV=test` by Quadlet drop-ins (which also forbid pulling, since every image, Postgres and SeaweedFS included, comes from an archive, and cut each unit's start timeout from production's 15 minutes to 5), and runs a Playwright smoke suite through Caddy at `ronote.test` and `ronote-run.test`, with Caddy's internal CA, which Chromium trusts by its key. It also checks that the `ronote` user listens on 80 and 443 alone and nothing else answers, and runs `check-config` on `deploy/ronote.env.example` (which must pass) and on a same-site pair (which must fail). `make prod-local` runs the same units in the podman machine's VM (Fedora CoreOS), and the smoke suite from the Mac through an ssh tunnel.

## Consequences

- A deployment is a host, two domains, a GitHub OAuth app and the secrets; `deploy/install.sh` and `check-config` cover the rest.
- One host means one API process, so the in-memory rate limits and Stop (ADR 0010, ADR 0012) stay correct.
- Backups need Postgres and the blob store (ADR 0013); ADR 0015 sets them up.
- docling's 4 GB is below what dev gives it; the amd64 smoke job runs a fake, so only the weekly arm64 job with the real images proves the limit (ADR 0015).
- `install.sh` refuses a host whose PAM gives the `ronote` user's systemd another user's `XDG_*` directories, as GitHub's runner image does for its own user in `/etc/environment`: Quadlet and every podman would use that user's directories. It never overrides them; a host that trips it removes those lines from `/etc/environment` (or `/etc/security/pam_env.conf`) and restarts `user@<uid>.service`. CI does that before installing.
- What reaches users is what CI ran: the same units, the same Caddy config and the same images, with only the fakes swapped in.
