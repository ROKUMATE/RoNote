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

# With job control on, a background job leads a process group of its own.
# Its input is not the terminal, which a background group may not read.
set -m
"$@" </dev/null &
pid=$!
# The group is set; without job control the shell reports nothing about it.
set +m
echo "$pid" >"$file"
trap 'kill -s TERM -- "-$pid" 2>/dev/null' INT TERM HUP
status=0
wait "$pid" || status=$?
rm -f "$file"
exit "$status"
