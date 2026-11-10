#!/bin/sh
# Runs scripts/check-doc-sizes.sh against docs of known sizes. Exits non-zero
# if any case fails.
set -u

script=$(cd "$(dirname "$0")" && pwd)/check-doc-sizes.sh
work=$(mktemp -d "${TMPDIR:-/tmp}/doc-sizes-test.XXXXXX")
trap 'rm -rf "$work"' EXIT
passed=0
failed=0

# lines N WIDTH: N lines of WIDTH characters each, newline included.
lines() {
  awk -v n="$1" -v w="$2" 'BEGIN { for (i = 0; i < n; i++) { line = sprintf("%" (w - 1) "s", ""); gsub(/ /, "x", line); print line } }'
}

# matches: the output is TEXT's, or empty when TEXT is.
matches() {
  if [ -z "$text" ]; then
    [ -z "$out" ]
  else
    case $out in *"$text"*) true ;; *) false ;; esac
  fi
}

# expect NAME STATUS TEXT CONTEXT_LINES CONTEXT_WIDTH AGENTS_LINES AGENTS_WIDTH:
# the check exits with STATUS on docs of those sizes, and prints TEXT (none
# when TEXT is empty).
expect() {
  name=$1
  want=$2
  text=$3
  lines "$4" "$5" >"$work/CONTEXT.md"
  lines "$6" "$7" >"$work/AGENTS.md"
  sh "$script" "$work" >"$work/out" 2>&1
  status=$?
  out=$(cat "$work/out")
  if [ "$status" -eq "$want" ] && matches; then
    passed=$((passed + 1))
    printf 'ok    %s\n' "$name"
  else
    failed=$((failed + 1))
    printf 'FAIL  %s\n      exit %s, want %s\n      output: %s\n' "$name" "$status" "$want" "$out"
  fi
}

expect 'both within their sizes' 0 '' 80 76 150 68
expect 'CONTEXT.md one line over' 1 'CONTEXT.md is 81 lines and 6156 bytes, over its 80 lines and 6144 bytes' 81 76 10 10
expect 'CONTEXT.md one byte over' 1 'CONTEXT.md is 1 lines and 6145 bytes' 1 6145 10 10
expect 'AGENTS.md one line over' 1 'AGENTS.md is 151 lines' 10 10 151 10
expect 'AGENTS.md one byte over' 1 'AGENTS.md is 1 lines and 10241 bytes, over its 150 lines and 10240 bytes' 10 10 1 10241

rm "$work/AGENTS.md"
sh "$script" "$work" >"$work/out" 2>&1
status=$?
if [ "$status" -eq 1 ] && grep -q 'AGENTS.md is missing' "$work/out"; then
  passed=$((passed + 1))
  printf 'ok    a missing doc\n'
else
  failed=$((failed + 1))
  printf 'FAIL  a missing doc\n      exit %s, output: %s\n' "$status" "$(cat "$work/out")"
fi

printf '\n%s passed, %s failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
