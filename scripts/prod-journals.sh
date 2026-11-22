#!/bin/sh
# What ronote's units logged since boot, and their containers' states, saved
# for a failed CI run to upload (make prod-journals; run it as root):
#
#   sudo sh scripts/prod-journals.sh [FILE]        FILE defaults to bin/prod-journals.log
#
# When no unit was ever installed, because an earlier step failed first, the
# file says "no units installed" and the script succeeds: one failure to read,
# not two. The journal holds no secret, since the units log none.
set -u

here=$(cd "$(dirname "$0")" && pwd)
out=${1:-bin/prod-journals.log}
mkdir -p "$(dirname "$out")"

# as_user runs a command as ronote; a test stands in with RONOTE_AS_RONOTE.
as_user() {
	sh "${RONOTE_AS_RONOTE:-$here/../deploy/as-ronote.sh}" "$@"
}

if id ronote >/dev/null 2>&1 && [ -n "$(as_user systemctl --user list-unit-files --no-legend 'ronote-*' 2>/dev/null)" ]; then
	as_user journalctl --user -b --no-pager -o short-precise -u 'ronote-*' >"$out" 2>&1
	as_user podman ps -a --format '{{.Names}} {{.Status}}' >>"$out" 2>&1
else
	printf 'no units installed\n' >"$out"
fi
# Run under sudo, the file would be root's; the job's user uploads it.
[ -z "${SUDO_UID:-}" ] || chown "$SUDO_UID:${SUDO_GID:-$SUDO_UID}" "$out"
exit 0
