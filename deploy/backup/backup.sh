#!/bin/sh
# Backs RoNote up (docs/adr/0015-operations.md), as ronote, from its systemd
# timers or make backup-now:
#
#   sh backup.sh db     the database, nightly: pg_dump -Fc and each table's rows
#   sh backup.sh pdfs   the PDF bucket, weekly, through a mirror of it
#
# into the restic repository the podman secret ronote-backup-repo names,
# encrypted with ronote-restic-password, kept 7 daily, 4 weekly, 6 monthly.
# No secret passes through this script: podman gives each to restic and
# rclone as their environment, so none is on a command line or in output.
set -eu

work=${RONOTE_BACKUP_WORK:-$HOME/.local/state/ronote/backup}
name=backup
# shellcheck source=deploy/backup/lib.sh
. "$(dirname "$0")/lib.sh"

# The repository, made the first time: restic says 10 when there is none.
ready() {
	status=0
	restic -- cat config >/dev/null 2>&1 || status=$?
	case $status in
	0) ;;
	10) restic -- init >/dev/null && say "made the repository" ;;
	*) say "can't open the repository (restic exit $status)" >&2; exit 1 ;;
	esac
}

keep() {
	restic -- forget --tag "$1" --keep-daily 7 --keep-weekly 4 --keep-monthly 6 --prune --quiet
}

db() {
	rm -rf "$work"
	mkdir -p "$work"
	podman exec ronote-postgres pg_dump -U ronote -d ronote -Fc >"$work/ronote.dump"
	# The dump's own rows, which the restore drill compares the restored
	# tables with: read from the file by the same Postgres, so a pg_restore
	# that fails fails the backup (pipefail), and nothing goes through stdin.
	podman run --rm --log-driver=none --network=none --read-only --cap-drop=all --security-opt=no-new-privileges \
		-v "$work:/backup:ro,Z" -v "$(dirname "$0")/count-rows.awk:/count-rows.awk:ro,Z" \
		"$(podman container inspect --format '{{.ImageName}}' ronote-postgres)" \
		bash -o pipefail -c 'pg_restore --data-only -f - /backup/ronote.dump | awk -f /count-rows.awk' >"$work/counts.tsv"
	ready
	restic -v "$work:/backup:ro,Z" -- backup --tag db --host ronote --quiet /backup
	keep db
	say "database: $(wc -l <"$work/counts.tsv" | tr -d ' ') tables, $(wc -c <"$work/ronote.dump" | tr -d ' ') bytes"
	rm -rf "$work"
}

pdfs() {
	set -a
	# shellcheck source=/dev/null # the deployment's own settings
	. "$etc/ronote.env"
	set +a
	podman volume exists ronote-pdf-mirror || podman volume create ronote-pdf-mirror >/dev/null
	podman run --rm --log-driver=none --read-only --cap-drop=all --security-opt=no-new-privileges --memory=512m \
		--network ronote-egress --network ronote-data \
		--secret ronote-blob-access-key,type=env,target=RCLONE_CONFIG_BLOBS_ACCESS_KEY_ID \
		--secret ronote-blob-secret-key,type=env,target=RCLONE_CONFIG_BLOBS_SECRET_ACCESS_KEY \
		-e RCLONE_CONFIG_BLOBS_TYPE=s3 -e RCLONE_CONFIG_BLOBS_PROVIDER=Other \
		-e RCLONE_CONFIG_BLOBS_ENDPOINT="$RONOTE_BLOB_ENDPOINT" -e RCLONE_CONFIG_BLOBS_REGION="$RONOTE_BLOB_REGION" \
		-e RCLONE_CONFIG_BLOBS_FORCE_PATH_STYLE=true -e RCLONE_CONFIG_DIR=/tmp \
		--tmpfs /tmp -v ronote-pdf-mirror:/mirror:Z \
		"$(image rclone)" sync "blobs:$RONOTE_BLOB_BUCKET" /mirror --exclude '.ronote-check/**' --quiet
	ready
	restic -v ronote-pdf-mirror:/pdfs:ro,Z -- backup --tag pdfs --host ronote --quiet /pdfs
	keep pdfs
	say "PDFs: the bucket $RONOTE_BLOB_BUCKET"
}

case ${1:-} in
db) db ;;
pdfs) pdfs ;;
*) printf 'usage: sh backup.sh db|pdfs\n' >&2; exit 2 ;;
esac
