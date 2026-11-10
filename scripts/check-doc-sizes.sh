#!/bin/sh
# Fails when an agent doc outgrows its size. Every agent session reads
# CONTEXT.md and AGENTS.md whole, so they hold state and rules only; detail
# lives in docs/ (AGENTS.md). A KB is 1,024 bytes.
#
#   sh scripts/check-doc-sizes.sh [DIR]   DIR holds the docs; default the repo root
set -u

dir=${1:-$(cd "$(dirname "$0")/.." && pwd)}
status=0

# check FILE MAX_LINES MAX_BYTES: reports FILE when it has more of either.
check() {
  file=$dir/$1
  if [ ! -f "$file" ]; then
    echo "check-doc-sizes: $1 is missing" >&2
    status=1
    return
  fi
  lines=$(wc -l <"$file" | tr -d ' ')
  bytes=$(wc -c <"$file" | tr -d ' ')
  if [ "$lines" -gt "$2" ] || [ "$bytes" -gt "$3" ]; then
    echo "check-doc-sizes: $1 is $lines lines and $bytes bytes, over its $2 lines and $3 bytes; move detail to docs/" >&2
    status=1
  fi
}

check CONTEXT.md 80 6144
check AGENTS.md 150 10240
exit "$status"
