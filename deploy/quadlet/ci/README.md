# CI and prod-local drop-ins

The production units as CI runs them (docs/adr/0014-production-topology.md):
`deploy/install.sh --drop-ins deploy/quadlet/ci` installs these beside the
units, and `config/` as the deployment's configuration. A second `--drop-ins`
adds a layer over this one: `ci-fakes`, for the smoke jobs and `make
prod-local`, or `real`, for the weekly real-image job (`.github/workflows/arm-real.yml`).

- `../ci-fakes` makes `ronote-ollama`, `ronote-docling` and `ronote-pdftext` run
  `cmd/fakeembed` and `cmd/fakedocling` from the `ronote-fakes` image instead of
  their real images.
- `../real` leaves those three on their real images, which their start pulls
  (`Pull=missing`) within 25 minutes, not 5; its files are named `z-real.conf`
  because Quadlet applies drop-ins by file name, and they must sort after
  `start.conf` below. The memory limits and health checks stay production's.
- `ronote-fakellm` and `ronote-fakeidp` are the model and GitHub; the fake
  sign-in is behind Caddy as `idp.ronote.test`, since the browser goes there.
- `ronote-.container.d/start.conf` applies to every unit: `Pull=never` (every
  image comes from the archive `make prod-config` saves, so a missing one fails
  at once, by name, and nothing waits on Docker Hub) and `TimeoutStartSec=300`,
  where production has 900. It needs Quadlet 5.2; `make units-check` checks it.
- `ronote-seaweedfs` is CI's blob store (production's is Oracle's Object
  Storage), and `seaweedfs.conf` starts the api and the worker after it: its
  health check makes the bucket they check as they start.
- Postgres and SeaweedFS run the images their units pin, saved under local names
  (`localhost/ronote-ci/<name>:pinned`) that `ronote-<name>.container.d/image.conf`
  names: a saved image keeps no digest reference, so the pin would make podman pull.
- `config/backup.images` names the backups' restic and rclone by the local tags
  `make prod-config` gives their pins; the repository is on the host
  (`RONOTE_BACKUP_REPO=/var/lib/restic/repo`, in `ronote-backup`'s volume).
- `RONOTE_ENV=test`: staging and prod refuse a fake sign-in.
- Caddy serves the runner too (`config/sites.caddy` imports `runner.caddy`),
  which production has on Cloudflare Pages.
- Caddy issues its own certificates (`local_certs`) for `ronote.test`,
  `ronote-run.test` and `idp.ronote.test`, and serves the pages the smoke
  suite imports to the worker at `http://ronote-caddy:8080`.
