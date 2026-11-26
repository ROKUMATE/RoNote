# Go-live and operations runbook

How RoNote goes onto its production host and stays there (docs/adr/0015-operations.md), in order. Each step gives the command or the console path, then how to check it. The commands are those of the scripts as they are: `deploy/bootstrap.sh`, `deploy/install.sh`, `deploy/ronote-deploy.sh`, `deploy/host-swap.sh`, `deploy/pasta-apparmor.sh`, `.github/workflows/release.yml`, `deploy/backup/` and `deploy/alerts/`. Console paths for Oracle, Cloudflare, GitHub, Anthropic and Namecheap come from their documentation and were not tried here, so each step ends in a check that shows a path that has moved. What is still to decide, or what the scripts don't do yet, is in docs/open-questions.md, not here.

Conventions:
- `$DOMAIN` is the app's domain (`ronote.example.me`), `$PROJECT` the Pages project (`ronote-runner`), `$HOST` the instance's public IP, and `$TAG` a release (`v1.0.0`).
- `as-ronote` is short for `sudo sh /usr/local/lib/ronote/as-ronote.sh`, which bootstrap installs, so the units' own `systemctl --user`, `journalctl --user` and `podman` work as ronote.
- Secrets never go on a command line or into output. Type them at `read -rs`, which echoes nothing, and keep each in the password manager: the host is not where they live.

## Before the instance

1. **The domain.** GitHub Student Developer Pack → Namecheap → claim a `.me` domain. Its A record waits for the instance's IP ("The instance", step 5).
2. **Cloudflare.**
   1. Sign up; Account Home shows the account ID, for `CLOUDFLARE_ACCOUNT_ID`.
   2. The runner's Pages project, from a checkout. The release deploys it; nothing is deployed by hand:

      ```sh
      pnpm install --frozen-lockfile --dir deploy/pages
      pnpm --dir deploy/pages exec wrangler login
      pnpm --dir deploy/pages exec wrangler pages project create $PROJECT --production-branch main
      pnpm --dir deploy/pages exec wrangler pages project list   # check: $PROJECT listed
      ```

   3. R2 → Create bucket `ronote-backups`, location automatic.
   4. R2 → Manage R2 API Tokens → Create API token: Object Read & Write, applied to `ronote-backups` only. Keep its Access Key ID and Secret Access Key. restic's repository is `s3:https://<ACCOUNT_ID>.r2.cloudflarestorage.com/ronote-backups`.
   5. My Profile → API Tokens → Create Token → Account → Cloudflare Pages → Edit: the release's `CLOUDFLARE_API_TOKEN`.
3. **The ntfy topic.** Generate a long random name in the password manager. Install the ntfy app on your phone and subscribe to `https://ntfy.sh/<the name>`. Anyone who knows the name can read the alerts and post to them, so it is a secret (ADR 0015).
4. **The GitHub OAuth app.** GitHub → Settings → Developer settings → OAuth Apps → New OAuth App:
   - Homepage URL `https://$DOMAIN`.
   - Authorization callback URL `https://$DOMAIN/api/v1/auth/github/callback`. The api derives the same from `RONOTE_PUBLIC_URL` (`cmd/api/main.go`), and a mismatch fails sign-in with GitHub's redirect error.
   - Keep the client ID for `ronote.env` and the generated client secret.
5. **Chat.**
   - Anthropic Console → API Keys → Create Key, for `RONOTE_ANTHROPIC_API_KEY`.
   - Pick the daily cap for all users together, in dollars: `RONOTE_CHAT_DAILY_USD` in `ronote.env` (the example's is 0.20).
   - Console → Billing → limits as a backstop, above the cap.
   - Without chat, set `RONOTE_CHAT_ENABLED=false` and leave the key out.
6. **The GitHub environment `production`.**
   1. The release's deploy key, made on your machine:

      ```sh
      ssh-keygen -t ed25519 -N '' -C ronote-deploy -f ronote-deploy
      ```

   2. Repo → Settings → Environments → New environment `production`:
      - Required reviewers: you.
      - Deployment branches and tags: tags matching `v*`.
      - Secrets: `CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_ACCOUNT_ID`, and `DEPLOY_SSH_KEY` (the private key's contents; then delete the file).
      - Variables: `RONOTE_PUBLIC_URL` `https://$DOMAIN` and `CLOUDFLARE_PAGES_PROJECT` `$PROJECT`. `DEPLOY_HOST` and `DEPLOY_KNOWN_HOSTS` come once the instance has an IP ("The instance", step 5).

   Check:

   ```sh
   gh api repos/ROKUMATE/RoNote/environments/production --jq '[.protection_rules[].type]'   # ["required_reviewers", ...]
   gh secret list --env production    # the three secrets
   ```

## The instance

1. **Create it.** Oracle Console → Compute → Instances → Create instance:
   - Image: Canonical Ubuntu 26.04, aarch64. `bootstrap.sh` refuses any other release.
   - Shape: `VM.Standard.A1.Flex`, 2 OCPUs and 12 GB, the whole Always Free A1 allowance (ADR 0014).
   - Boot volume: 100 GB rather than the 47 GB default. One copy of the stack's images is about 10.75 GB, and every update keeps the previous release's images for a rollback.
   - Your SSH public key, and a public IPv4 address.

   ```sh
   ssh ubuntu@$HOST 'lsb_release -ds; uname -m; nproc; free -g | sed -n 2p; df -h / | tail -1'
   # Ubuntu 26.04 LTS, aarch64, 2, about 11 GB total, about 97 GB
   ```

2. **Ingress.** Networking → Virtual cloud networks → the instance's VCN → Security Lists → Default Security List → Add Ingress Rules, each from `0.0.0.0/0`: TCP 80, TCP 443, and UDP 443 (HTTP/3). TCP 22 is there already. Oracle's image also rejects 80 and 443 in its own iptables rules, which bootstrap opens (`deploy/open-web-ports.sh`).
3. **Object Storage, for the PDFs.**
   1. Storage → Object Storage → Buckets → Create bucket `ronote-pdfs`: Standard, private, in the compartment that Tenancy details → Object Storage Settings names for the S3 compatibility API.
   2. Tenancy details → Object Storage namespace, and the region, make the endpoint `https://<namespace>.compat.objectstorage.<region>.oraclecloud.com`: `RONOTE_BLOB_ENDPOINT`, `RONOTE_BLOB_REGION` and `RONOTE_BLOB_BUCKET` in `ronote.env`.
   3. Profile → User settings → Customer secret keys → Generate secret key `ronote`. The secret shows once: keep it with its access key, for `RONOTE_BLOB_ACCESS_KEY` and `RONOTE_BLOB_SECRET_KEY`.

   The api's and the worker's `check-config` write to the bucket as they start, so a wrong endpoint or key stops the install with its reason.
4. **The configuration.** On the host, a checkout of the release about to be tagged ("First release", step 1), and its configuration:

   ```sh
   git clone https://github.com/ROKUMATE/RoNote.git ~/ronote
   mkdir -p ~/ronote-config && cp ~/ronote/deploy/ronote.env.example ~/ronote-config/ronote.env
   ```

   In `~/ronote-config/ronote.env`, set:
   - `RONOTE_PUBLIC_URL` `https://$DOMAIN` and `RONOTE_RUNNER_URL` `https://$PROJECT.pages.dev`;
   - `RONOTE_GITHUB_CLIENT_ID`;
   - the three `RONOTE_BLOB_*` settings;
   - `RONOTE_CHAT_ENABLED` and `RONOTE_CHAT_DAILY_USD`.
5. **DNS, and the deploy variables.**
   - Namecheap → Domain List → Manage → Advanced DNS: an `A` record for `@` with `$HOST`, and no `AAAA` record, as the instance has no IPv6.
   - In `production`'s variables: `DEPLOY_HOST` `$HOST`, and `DEPLOY_KNOWN_HOSTS`, the output of `ssh-keyscan -t ed25519 $HOST`.

   ```sh
   dig +short A $DOMAIN    # $HOST
   ```

## First release

1. **Tag it.**

   ```sh
   git tag -s $TAG && git push origin $TAG
   ```

   - The Release workflow's Images job pushes `ronote-api`, `ronote-worker` and `ronote-web` for amd64 and arm64.
   - Its Deploy job then waits in `production` for your approval, which comes after step 3: the job also deploys the runner's Pages site, and it reaches the host over SSH.
2. **Bootstrap**, on the host, from the tag. Check out the tag in `~/ronote` first (`git -C ~/ronote fetch --tags && git -C ~/ronote checkout $TAG`). Then the secrets, typed at prompts that echo nothing:

   ```sh
   cd ~/ronote
   for v in RONOTE_GITHUB_CLIENT_SECRET RONOTE_ANTHROPIC_API_KEY RONOTE_BLOB_ACCESS_KEY RONOTE_BLOB_SECRET_KEY \
            RONOTE_RESTIC_PASSWORD RONOTE_BACKUP_REPO RONOTE_BACKUP_ACCESS_KEY RONOTE_BACKUP_SECRET_KEY RONOTE_NTFY_TOPIC; do
     printf '%s: ' "$v"; read -rs value; echo; export "$v=$value"
   done; unset value
   sudo --preserve-env=RONOTE_GITHUB_CLIENT_SECRET,RONOTE_ANTHROPIC_API_KEY,RONOTE_BLOB_ACCESS_KEY,RONOTE_BLOB_SECRET_KEY,RONOTE_RESTIC_PASSWORD,RONOTE_BACKUP_REPO,RONOTE_BACKUP_ACCESS_KEY,RONOTE_BACKUP_SECRET_KEY,RONOTE_NTFY_TOPIC \
     sh deploy/bootstrap.sh --deploy-key ~/ronote-deploy.pub -- --version $TAG --config ~/ronote-config
   exit   # the secrets leave with the shell; they live on as podman secrets
   ```

   - **What to type:**
     - The restic password and the ntfy topic come from the password manager. Lose the restic password and the backups are unreadable.
     - `RONOTE_BACKUP_REPO` is restic's `s3:https://…` URL from "Before the instance", step 2.
     - `~/ronote-deploy.pub` is the deploy key's public half, copied to the host with `scp`.
   - **What bootstrap does:**
     1. Podman and pasta, with unattended upgrades;
     2. pasta's AppArmor rule;
     3. the 2 GiB swap file;
     4. SSH by key alone;
     5. ports 80 and 443 opened;
     6. the deploy user, and `ronote-deploy`'s clone in `/opt/ronote/repo.git`;
     7. `install.sh`, which ends with every unit healthy, or a report naming the one that isn't.
   - **Images:** the host pulls them from ghcr.io by the tag. Whether the packages are public is in docs/open-questions.md.
3. **Approve the Deploy job** in Actions. It deploys the runner's site to Pages, then runs `ronote-deploy $TAG` on the host. That command pins the images by digest, migrates, checks the schema, and waits for health.
4. **Check the app**, as a user would:

   ```sh
   curl -s https://$DOMAIN/healthz          # names $TAG
   sudo cat /var/lib/ronote/deploy/current  # tag=$TAG, and its pins
   ```

   Then, at `https://$DOMAIN`:
   1. Sign in with GitHub.
   2. Import a page, such as `https://docs.python.org/3/tutorial/controlflow.html`, and a PDF of your own.
   3. Run one of the page's Python blocks, and a JavaScript block from a page that has one.
   4. Search for a phrase in it.
   5. Ask the chat one question about it: the answer cites its blocks.

   Two more checks from your machine:
   - **The certificate:** `echo | openssl s_client -connect $DOMAIN:443 -servername $DOMAIN 2>/dev/null | openssl x509 -noout -issuer -enddate` shows Let's Encrypt as the issuer and an end date about 90 days out.
   - **Real client IPs through pasta:** request `https://$DOMAIN/api/v1/me` (it answers 401), then on the host run `as-ronote journalctl --user -u ronote-api --since -5min -o cat | grep -o '"client_ip":"[^"]*"' | tail -1`. It must be your public IP, not `10.89.x.x`.
5. **The runner's headers on Pages**, with its remove-then-add CSP rule.
   - Every file gets one CSP.
   - Only the JavaScript worker's script allows `'unsafe-eval'`.
   - The rest allow `'wasm-unsafe-eval'`.

   ```sh
   base=https://$PROJECT.pages.dev
   index=$(curl -s $base/ | grep -o '/assets/index-[A-Za-z0-9_-]*\.js' | head -1)
   worker=/assets/$(curl -s $base$index | grep -o 'javascript\.worker-[A-Za-z0-9_-]*\.js' | head -1)
   for path in / "$index" "$worker"; do
     printf '%s: %s CSP, ' "$path" "$(curl -sI "$base$path" | grep -ci '^content-security-policy')"
     curl -sI "$base$path" | grep -i '^content-security-policy' | grep -o "'[a-z-]*eval'"
   done
   # /: 1 CSP, 'wasm-unsafe-eval'; the index script: the same; the worker: 1 CSP, 'unsafe-eval'
   ```

6. **Docling at the page cap.**
   - **The baseline:** CI converted a 100-page PDF of code and tables in about 187 s, holding about 2.85 GiB of anon memory. That is GitHub's arm64 runner, ARM run 45578157125. The cap, `docling.MaxPages`, is 100 (ADR 0013).
   - **Expect slower here:** production's Ampere Altra cores are older, and docling shares the host's 2 OCPUs. The cap allows for half the speed.

   Print the same PDF on your machine, from a checkout, and copy it over:

   ```sh
   pnpm --dir apps/web exec node scripts/worst-pdf.ts $PWD/book-100.pdf 100 ../..
   scp book-100.pdf ubuntu@$HOST:
   ```

   Then convert it on the host, once, through the worker's own client. The worker image's binary is `/ronote`:

   ```sh
   time as-ronote podman exec -i ronote-worker /ronote convert-check < ~/book-100.pdf    # convert-check: ok
   as-ronote sh -c 'cat /sys/fs/cgroup/user.slice/user-$(id -u).slice/user@$(id -u).service/app.slice/ronote-docling.service/libpod-payload-*/memory.peak'
   as-ronote journalctl --user -u ronote-docling --since -15min -o cat | tail -20
   ```

   Compare the time with 187 s and the peak with about 2.85 GiB (3,060,000,000 bytes).
   - **The cap holds** if the time is under 540 s and the peak under 3 GiB.
   - **Lower the cap in a release** if the time is over 270 s, half of docling's timeout. Change `docling.MaxPages`, with docling's `DOCLING_SERVE_MAX_NUM_PAGES` and the web app's `maxPdfPages`; `deploy/units_test.go` fails until all three agree.
7. **The first backup, and a restore drill.** The timers run them nightly and weekly; the first ones run now, from the release that `ronote-deploy` unpacked:

   ```sh
   cd /var/lib/ronote/deploy/releases/$TAG
   sudo sh deploy/run-now.sh ronote-backup@db.service ronote-backup@pdfs.service
   sudo sh deploy/run-now.sh ronote-restore-drill.service
   as-ronote systemctl --user start ronote-alert@test.service   # the phone shows "test failed"
   as-ronote systemctl --user list-timers 'ronote-*'           # backup-db, backup-pdfs, restore-drill, watch
   ```

   - **The backups** make the restic repository on their first run and log their snapshots. A failure here is R2's token, or the repository's URL.
   - **The drill** restores the latest dump into a scratch Postgres and checks the schema with `api migrate status`. It compares every table's row count with the backup's, runs one search, and removes the scratch database.

## Day 2

**The units**, each a Quadlet unit of the `ronote` user (`deploy/quadlet/`), with its memory limit and the peak ARM run 45606851983 measured (`make prod-memory`; ADR 0014):

| unit | does | limit | measured peak | CPU weight |
|---|---|---|---|---|
| `ronote-caddy` | serves the app, with Let's Encrypt | 128 MB | 56 MiB | 1000 |
| `ronote-api` | the HTTP API | 256 MB | 11 MiB | 1000 |
| `ronote-worker` | imports and indexing | 512 MB | 20 MiB | 50 |
| `ronote-migrate` | migrations, once per start | 128 MB | 19 MiB | 100 |
| `ronote-postgres` | the database | 1 GB | 135 MiB | 1000 |
| `ronote-ollama` | embeds documents, a chunk at a time | 2 GB | 1,762 MiB (anon 1,170) | 50 |
| `ronote-ollama-query` | embeds searches' queries, from `ronote-ollama`'s volume, read-only | 1.5 GB | not yet measured | 1000 |
| `ronote-docling` | converts PDFs | 4 GB | 3,144 MiB at the 100-page cap | 50 |
| `ronote-pdftext` | reads PDFs' text layers | 256 MB | 72 MiB | 50 |

Under contention the units with weight 1000 go first, so a search's embedding isn't held up by an import; the backups' and the drill's containers yield too (ADR 0014). `ronote-ollama-query` starts after `ronote-ollama`, and the api after both. The api checks that both serve the model with one digest; until they have both answered, and for good if they differ, search is by words alone, and a mismatch alerts.

**Deploying a release.** Tag it (`git tag -s $TAG && git push origin $TAG`), then approve the Deploy job. `ronote-deploy` pins the images, migrates, restarts the units and waits until each is healthy and `/healthz` names the tag. If anything fails, it goes back to the previous release by itself and alerts "deploying $TAG failed". By hand, on the host: `sudo ronote-deploy $TAG`.

**Rolling back.** Migrations are expand/contract, so the previous release runs on the migrated schema (ADR 0015):

```sh
sudo cat /var/lib/ronote/deploy/previous    # tag=..., the release before
sudo ronote-deploy <that tag>
curl -s https://$DOMAIN/healthz              # names it
```

The runner's site rolls back on its own: Cloudflare → Workers & Pages → `$PROJECT` → Deployments → the previous one → Rollback.

**Rotating a secret.**
- `install.sh` keeps a podman secret that exists. So a rotation removes the secret, then reinstalls the current release with the new value in its environment.
- Reinstalling, rather than redeploying the same tag, keeps the previous release as the rollback target.

```sh
tag=$(sudo sed -n 's/^tag=//p' /var/lib/ronote/deploy/current)
pins=$(sudo sed -n 's/^pins=//p' /var/lib/ronote/deploy/current)
printf 'new value: '; read -rs NEW; echo
as-ronote podman secret rm ronote-github-client-secret
sudo env RONOTE_GITHUB_CLIENT_SECRET="$NEW" sh /var/lib/ronote/deploy/releases/$tag/deploy/install.sh --version $tag $pins
unset NEW
```

| secret | its variable | the new value | afterwards |
|---|---|---|---|
| `ronote-github-client-secret` | `RONOTE_GITHUB_CLIENT_SECRET` | the OAuth app → Generate a new client secret | delete the old one there; sign in |
| `ronote-anthropic-api-key` | `RONOTE_ANTHROPIC_API_KEY` | Anthropic Console → API Keys | disable the old key; ask one question |
| `ronote-blob-access-key`, `ronote-blob-secret-key` | `RONOTE_BLOB_ACCESS_KEY`, `RONOTE_BLOB_SECRET_KEY` | a second Customer Secret Key (a user may hold two) | delete the old key; import a PDF |
| `ronote-backup-access-key`, `ronote-backup-secret-key` | `RONOTE_BACKUP_ACCESS_KEY`, `RONOTE_BACKUP_SECRET_KEY` | a new R2 token | delete the old token; run a backup |
| `ronote-ntfy-topic` | `RONOTE_NTFY_TOPIC` | the password manager's generator | subscribe the phone to it; send the test alert |
| `ronote-docling-api-key`, `ronote-pdftext-api-key` | none | `install.sh` makes new random ones | import a PDF |

For a pair, remove both before reinstalling.

Two secrets need more than a reinstall:
- **The Postgres password.** `install.sh` makes it only when there is none, so change it in Postgres first, then replace both secrets that hold it:

  ```sh
  as-ronote podman exec -it ronote-postgres psql -U ronote -d ronote -c '\password ronote'   # type the new one twice
  printf 'the same password: '; read -rs NEW; echo
  as-ronote podman secret rm ronote-postgres-password ronote-database-url
  printf '%s' "$NEW" | as-ronote podman secret create ronote-postgres-password -
  printf 'postgres://ronote:%s@ronote-postgres:5432/ronote?sslmode=disable' "$NEW" | as-ronote podman secret create ronote-database-url -
  unset NEW
  as-ronote systemctl --user restart ronote-api.service ronote-worker.service
  ```

  Then run the restore drill, which reads the dump through the new URL.
- **The restic password.** restic keeps more than one key per repository, so add the new key before removing the old. Each step is `restic` in the backups' own image, as ronote, with the repository's secrets:

  ```sh
  as-ronote sh -c 'cd /tmp && podman run --rm -it --network ronote-egress \
    --secret ronote-restic-password,type=env,target=RESTIC_PASSWORD --secret ronote-backup-repo,type=env,target=RESTIC_REPOSITORY \
    --secret ronote-backup-access-key,type=env,target=AWS_ACCESS_KEY_ID --secret ronote-backup-secret-key,type=env,target=AWS_SECRET_ACCESS_KEY \
    $(sed -n "s/^restic=//p" ~/.config/ronote/backup.images) key add'     # type the new password twice
  ```

  Then remove the `ronote-restic-password` secret and reinstall with `RONOTE_RESTIC_PASSWORD`, as above. Run `key list`, then `key remove <the old key's ID>`, the same way. Finally run a backup.

**The deploy key.**
1. Make a new pair.
2. Run `sudo sh ~/ronote/deploy/bootstrap.sh --deploy-key new.pub -- --version $tag $pins`. It rewrites `authorized_keys` and reinstalls the current release as it is.
3. Put the private half in `DEPLOY_SSH_KEY`.

**Reading the journals.** Every unit logs to ronote's user journal:

```sh
as-ronote journalctl --user -u 'ronote-*' --since -1h --no-pager       # everything, the last hour
as-ronote journalctl --user -u ronote-api -f                           # one unit, following
as-ronote systemctl --user list-units 'ronote-*' --state=failed        # what is down
as-ronote podman ps -a --format '{{.Names}} {{.Status}}'               # the containers and their health
```

journald keeps at most 1 GB.

**The alerts.** All come to the ntfy topic. The health watch checks every 5 minutes (`deploy/alerts/watch.sh`); each condition alerts once, again a day later while it lasts, and afresh after it clears.

| title | means | do |
|---|---|---|
| the disk is N% full | the root volume is over 80% (the watch's threshold, ADR 0015) | `as-ronote podman system df` and `as-ronote podman images`: remove releases' images older than `/var/lib/ronote/deploy/previous`'s with `as-ronote podman rmi`, keeping that one for a rollback; or grow the boot volume |
| `<unit>` is down | a unit not active, or its container unhealthy, for 5 minutes | read its journal; `as-ronote systemctl --user status <unit>` |
| `<domain>`'s certificate expires within 14 days | Caddy hasn't renewed it | `as-ronote journalctl --user -u ronote-caddy`: usually DNS or port 80 |
| search is by words alone: the embedders serve different models | `ronote-ollama` and `ronote-ollama-query` don't list the model with one digest, or one lacks it; search has no meaning half until the api restarts | `as-ronote journalctl --user -u ronote-api` names both; check both read the one volume, then restart the api |
| chat reached today's spend cap | `RONOTE_CHAT_DAILY_USD` is spent; chat is off for everyone until midnight UTC | nothing, or raise the cap and restart the api |
| backup-db failed, backup-pdfs failed | a nightly or weekly backup failed | `as-ronote journalctl --user -u 'ronote-backup@*' --since -1d`; then run it again |
| restore-drill failed | the weekly drill's restore, schema check, counts or search failed | its journal; the backups may not restore |
| deploying `<tag>` failed | the release failed its checks; the previous one runs again, or, if it says so, going back failed too | `as-ronote journalctl --user -u 'ronote-*' --since -1h` |
| test failed | the test alert above | nothing |

**The swap file.**
- **What it is:** bootstrap gave the host a 2 GiB `/swapfile` with `vm.swappiness=10` (`deploy/host-swap.sh`). It is for when the units' peaks coincide; the limits fit without it (ADR 0014). A host with swap of its own keeps that.
- **Check:** `swapon --show` lists `/swapfile` 2G, and `sysctl vm.swappiness` is 10.
- **Watch:** swap in use for long means memory is short. The memory report's swap column in the weekly ARM job is the measure.

**passt and its AppArmor rule.**
- **Why the rule:** bootstrap adds upstream's `signal (receive) peer=podman,` to `/etc/apparmor.d/usr.bin.pasta` (`deploy/pasta-apparmor.sh`, ADR 0015). Without it, every container stop fails with `rootless netns: kill network process: permission denied`.
- **The cost:** the edited profile is a dpkg conffile. A passt update that changes it stops at a conffile prompt, which unattended upgrades won't answer, so that update waits.
- **Now and then:** check `apt-cache policy passt` for a newer candidate. When there is one, install it by hand, taking the package's profile, then run the script again:

  ```sh
  sudo apt-get install -o Dpkg::Options::=--force-confnew passt
  sudo sh ~/ronote/deploy/pasta-apparmor.sh
  ```

  - If the new profile lacks the rule, the script adds it again and reloads it.
  - If it says `already lets podman signal pasta`, Ubuntu has shipped the fix. `pasta-apparmor.sh` and its line in `bootstrap.sh` can then go, in a `Chore:` commit, once every host has that passt.

**Dependencies.** Renovate opens weekly grouped pull requests (`renovate.json`): Go modules, web dependencies, GitHub Actions and container images. Each is pinned by digest, and none is merged by itself. Some carry notes on what to run again before merging, such as docling's (`make pdf-record`, the ARM job).
