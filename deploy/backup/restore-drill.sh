#!/bin/sh
# The weekly restore drill (docs/adr/0015-operations.md), as ronote, from its
# systemd timer or make restore-drill: the newest database backup restored
# into a scratch Postgres, its schema checked with api migrate status, each
# table's rows compared with what the backup counted, and one search run.
# It says how long the restore took, the measure of the RTO, and fails on
# any difference; the scratch database never outlives it. No secret passes
# through this script (see backup.sh).
set -eu

work=${RONOTE_BACKUP_WORK:-$HOME/.local/state/ronote/drill}
name=restore-drill
# shellcheck source=deploy/backup/lib.sh
. "$(dirname "$0")/lib.sh"

scratch=ronote-drill-postgres
network=ronote-drill
# The scratch database's own password, made for this drill, on a network
# nothing else is on.
password=$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')
url="postgres://ronote:$password@$scratch:5432/ronote?sslmode=disable"

cleanup() {
	podman rm -f -v "$scratch" >/dev/null 2>&1 || true
	podman network rm -f "$network" >/dev/null 2>&1 || true
	rm -rf "$work"
}
trap cleanup EXIT
cleanup

started=$(date +%s)
# The images the running units use, so the drill checks this release.
postgres=$(podman container inspect --format '{{.ImageName}}' ronote-postgres)
api=$(podman container inspect --format '{{.ImageName}}' ronote-api)

mkdir -p "$work"
# Contents only: a rootless container may not set the files' SELinux labels.
restic -v "$work:/restore:Z" -- restore latest --tag db --target /restore --exclude-xattr '*' --quiet
dump=$work/backup/ronote.dump
[ -s "$dump" ] || { say "the newest backup has no database dump" >&2; exit 1; }

podman network create --internal "$network" >/dev/null
podman run -d --name "$scratch" --network "$network" --memory=1g --log-driver=none \
	-v "$work:/restore:ro,Z" \
	-e POSTGRES_USER=ronote -e POSTGRES_PASSWORD="$password" -e POSTGRES_DB=ronote \
	--mount type=volume,dst=/var/lib/postgresql \
	--health-cmd 'pg_isready -U ronote -d ronote' --health-interval 2s \
	"$postgres" >/dev/null
podman wait --condition=healthy "$scratch" >/dev/null
podman exec "$scratch" pg_restore -U ronote -d ronote --exit-on-error --no-owner /restore/backup/ronote.dump
restored=$(($(date +%s) - started))

podman run --rm --log-driver=none --network "$network" -e RONOTE_DATABASE_URL="$url" "$api" migrate status

tables=0
wrong=
while IFS="$(printf '\t')" read -r table rows; do
	tables=$((tables + 1))
	got=$(podman exec "$scratch" psql -U ronote -d ronote -At -c "SELECT count(*) FROM $table")
	[ "$got" = "$rows" ] || wrong="$wrong $table ($got rows, the backup counted $rows)"
done <"$work/backup/counts.tsv"
[ -z "$wrong" ] || { say "the restored tables differ from the backup:$wrong" >&2; exit 1; }

podman run --rm --log-driver=none --network "$network" -e RONOTE_DATABASE_URL="$url" "$api" search-check
say "passed: $tables tables as the backup counted them, restored in ${restored}s"
