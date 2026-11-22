# 0015: Operations on Oracle and Cloudflare

Accepted, 2026-11-21.

## Context

ADR 0014 fixed the topology and proved it in CI, on no host in particular. Running it for users needs a host, a home for the runner, the PDFs and the backups, a way to hear that something broke, and a way to ship a release that can be undone. RoNote has no budget: everything here is on a free tier, and the choices must survive that tier going away.

## Decision

- **Host: Oracle Cloud Always Free**, one `VM.Standard.A1.Flex` with 2 OCPUs (Ampere, arm64) and 12 GB, on Ubuntu 26.04 LTS for aarch64.
  - Ubuntu 26.04 is the only supported release: its Podman 5.7 runs the units, and 24.04's Podman 4.9.3 rejects Quadlet's `Entrypoint=`, so `install.sh` refuses it (ADR 0014).
  - `deploy/bootstrap.sh` prepares a fresh host (Podman, a deploy user, unattended upgrades, SSH by key only with no root login, the firewall), then runs `install.sh`. Oracle's Ubuntu images end `/etc/iptables/rules.v4` with a catch-all `REJECT`, which blocks 80 and 443 even when the cloud security list allows them; bootstrap adds those ports before it.
- **Two sites.** The app on a `.me` domain; the runner on Cloudflare Pages at `<project>.pages.dev`. `pages.dev` is on the Public Suffix List (its private section, submitted by Cloudflare, Inc.), so each project under it is a registrable domain of its own, a different site from the app, as ADR 0008 requires; config checks the pair by the same list.
  - One definition of the runner's headers (`apps/web/runner/policy.ts`) generates both Caddy's runner block, which CI and `make prod-local` serve, and Pages' `_headers`; a test merges `_headers` as Pages does and fails unless every file gets exactly one CSP and only the JavaScript worker's script gets `'unsafe-eval'`.
  - The runner's `index.html` keeps its `ronote-app-url` meta tag; the release fills it with the app's URL, as Caddy does elsewhere. Pyodide's largest file, 9.6 MB, is well under Pages' 25 MiB a file.
  - A release deploys the runner with wrangler, from the GitHub environment `production`. Production Caddy serves only the app.
- **PDFs in Oracle Object Storage**, through its S3-compatible API, by config alone: the blob port already speaks path-style S3. The bucket is made by hand (docs/runbook.md); the api and the worker check at start that it exists and takes a write, and fail naming it. SeaweedFS stays in CI and `make prod-local` only.
- **Backups to Cloudflare R2, encrypted by restic**, under the `ronote` user's systemd timers:
  - nightly, `pg_dump -Fc` with the row count of every table, both from one snapshot;
  - weekly, a copy of the PDF bucket;
  - kept 7 daily, 4 weekly and 6 monthly;
  - the restic password and R2's keys are podman secrets.
  - **RPO 24 hours, RTO under 2 hours:** a new host from `bootstrap.sh`, `install.sh`, a restore and a deploy.
  - **A weekly restore drill** restores the latest dump into a scratch Postgres, checks the schema with `api migrate status`, compares every table's row count with the backup's, and runs one search. A failed drill alerts like a failed backup.
- **Alerts through ntfy.sh**, to a topic whose name is a podman secret: a failed backup or drill, a disk over 80%, a unit not active or unhealthy for 5 minutes, the certificate within 14 days of expiring, and chat's daily spend cap being reached (the api logs one line the first time each UTC day; the watcher reads it from the journal). journald keeps at most 1 GB.
- **Deploys are gated.** A `v*` tag builds the images, deploys the runner, then waits in the `production` environment for its required reviewer. The deploy job runs one command over SSH, as a deploy user whose key is forced to `ronote-deploy <tag>`:
  - it pulls the tag's images and pins them by digest;
  - it migrates, and checks the schema with `api migrate status`;
  - it restarts the units and waits until each is healthy and `/healthz` shows the tag's version;
  - on failure, it goes back to the previous digests (kept in a state file), and alerts.
  - **Migrations stay expand/contract**: a release only adds what the previous one ignores, and drops what it no longer uses one release later, so the previous release still runs on the migrated schema and a rollback never needs a down migration.
- **Proof in CI on arm64.** A job on `ubuntu-26.04-arm` runs `bootstrap.sh` and the units with the fakes on every pull request, as the amd64 smoke job does. A weekly job runs the real docling and Ollama under the production memory limits, imports a PDF and searches it, and reports each unit's peak memory.
- **Dependencies by Renovate**: weekly grouped pull requests, digests pinned, no automerge, one-line `Build: Bump <dep> to <version>` commits (ADR 0003). Dependabot's security alerts stay on.

## Trade-offs

- **Oracle's free tier against a paid host.** It costs nothing and its 12 GB fits the real docling, but it can be taken away:
  - idle Always Free instances can be reclaimed;
  - A1 capacity is often unavailable, so a lost instance may not come back soon in the same region;
  - an account can be closed.
  Backups off the provider, in R2, and a host that `bootstrap.sh` rebuilds from nothing cover all three; the RTO assumes a host can be had, on Oracle or any Ubuntu 26.04 host, arm64 or amd64.
- **The runner on Pages against Caddy.** Pages serves it from Cloudflare's edge, for free, on a domain that is a site of its own without buying one. The cost is two configurations to keep alike, hence one generated definition, and a deploy step that fills the app's URL.
- **Backups in R2 against Oracle's storage.** Off the provider is the point: Oracle's own storage would go with the account.
- **ntfy against email.** No mail server and no account; anyone who learns the topic can read or post to it, so its name is a secret and alerts carry no secrets.
- **pasta's AppArmor profile, edited.** Ubuntu 26.04 confines podman under a profile named `podman`, and passt's profile for pasta (`usr.bin.pasta`, passt 0.0~git20260120) takes no signal from it: podman can't stop pasta when the last container leaves the rootless network, so every stop, restart, deploy and rollback fails with `rootless netns: kill network process: permission denied` (`podman unshare --rootless-netns true` reproduces it). Upstream's fix, ["apparmor: Fix signaling issues from podman"](https://archives.passt.top/passt-dev/20260601230820.2c4cb7dd@elisabeth/t/) ([Launchpad 2154379](https://bugs.launchpad.net/ubuntu/+source/passt/+bug/2154379)), adds `signal (receive) peer=podman,` and is not yet merged. `deploy/pasta-apparmor.sh` adds exactly those lines and reloads that one profile; AppArmor stays on and pasta confined. The profile includes no `local/` file, so the lines go into the profile itself, a dpkg conffile: a passt update that changes the profile then stops at a conffile prompt, which unattended upgrades won't answer, so it is installed by hand with the package's profile (docs/runbook.md). Once the package's profile allows the signal, the script leaves it alone and can be removed.
- **No metrics endpoint.** Nothing would scrape it on one host; the alerts watch health (docs/decisions.md).

## Consequences

- A deployment is now: the Oracle instance, the `.me` domain, the Pages project, the R2 and Object Storage buckets, the GitHub OAuth app, the ntfy topic and the `production` environment, set up by docs/runbook.md.
- Only Postgres and the PDFs are backed up; the units, the configuration and the images come from the repo, a release tag and the runbook.
- docling's limit on arm64 is proven only by the weekly job, against one fixture PDF.
