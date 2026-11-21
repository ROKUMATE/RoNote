#!/bin/sh
# Runs ronote's one-shot units now, as their timers would, and prints what
# each logged this time (docs/adr/0015-operations.md): make backup-now and
# make restore-drill. Run it as root:
#
#   sudo sh deploy/run-now.sh ronote-backup@db.service ...
#
# It stops at the first unit that fails, and exits non-zero.
set -eu
here=$(cd "$(dirname "$0")" && pwd)

for unit in "$@"; do
	since=$(date +%s)
	status=0
	sh "$here/as-ronote.sh" systemctl --user start "$unit" || status=$?
	# What the script and systemd said; podman logs its containers' events too.
	sh "$here/as-ronote.sh" journalctl --user -u "$unit" --since "@$since" -t sh -t systemd --no-pager -o cat || true
	if [ "$status" -ne 0 ]; then
		printf 'run-now: %s failed\n' "$unit" >&2
		exit 1
	fi
done
