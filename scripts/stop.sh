#!/bin/sh
# Ends every process group run.sh recorded, children included: TERM, then
# KILL for any still there after 5 seconds.
#
# The groups are in $RONOTE_RUN_DIR (default .run/ in the repo).
set -eu

dir=${RONOTE_RUN_DIR:-$(cd "$(dirname "$0")/.." && pwd)/.run}
[ -d "$dir" ] || exit 0
for file in "$dir"/*.pgid; do
  [ -e "$file" ] || continue
  name=$(basename "$file" .pgid)
  pgid=$(cat "$file")
  if kill -s TERM -- "-$pgid" 2>/dev/null; then
    i=0
    while kill -s 0 -- "-$pgid" 2>/dev/null && [ "$i" -lt 50 ]; do
      sleep 0.1
      i=$((i + 1))
    done
    kill -s KILL -- "-$pgid" 2>/dev/null || true
    echo "stopped $name"
  fi
  rm -f "$file"
done
