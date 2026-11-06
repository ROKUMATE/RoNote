#!/bin/sh
# Runs a dev process in a process group of its own and records the group, so
# make stop can end it and everything it started, such as the binary go run
# builds. Ctrl-C ends the whole group too.
#
#   run.sh NAME CMD [ARGS...]
#
# The group goes in $RONOTE_RUN_DIR/NAME.pgid (default .run/ in the repo).
set -eu

[ "$#" -ge 2 ] || {
  echo 'usage: run.sh NAME CMD [ARGS...]' >&2
  exit 2
}
name=$1
shift
dir=${RONOTE_RUN_DIR:-$(cd "$(dirname "$0")/.." && pwd)/.run}
mkdir -p "$dir"
file=$dir/$name.pgid

# A non-interactive shell cannot enable job control portably. Start a new
# session explicitly so the command and its children share a killable group.
if command -v setsid >/dev/null 2>&1; then
  setsid "$@" </dev/null &
elif command -v python3 >/dev/null 2>&1; then
  python3 -c 'import os, sys; os.setsid(); os.execvp(sys.argv[1], sys.argv[1:])' "$@" </dev/null &
elif command -v perl >/dev/null 2>&1; then
  perl -MPOSIX -e 'POSIX::setsid() or die "setsid: $!\n"; exec @ARGV or die "exec: $!\n"' "$@" </dev/null &
else
  echo 'run.sh: no session launcher found (need setsid, python3, or perl)' >&2
  exit 1
fi
pid=$!
echo "$pid" >"$file"
trap 'kill -s TERM -- "-$pid" 2>/dev/null' INT TERM HUP
status=0
wait "$pid" || status=$?
rm -f "$file"
exit "$status"
