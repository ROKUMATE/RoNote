#!/bin/sh
# Every make target that names bin/ makes it first, in a checkout that has
# none: CI's, or a fresh clone, where ignored directories don't exist. The
# working tree's files that git tracks or would track are copied into an
# empty directory, and each target's dry run (make -n, which prints its
# prerequisites' recipes too) must print "mkdir -p bin" before its first
# line naming bin/. Exits non-zero if any target fails.
set -u

repo=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/fresh-bin-test.XXXXXX")
trap 'rm -rf "$work"' EXIT
fresh=$work/repo
mkdir "$fresh"
(cd "$repo" && git ls-files -co --exclude-standard | while read -r f; do [ ! -f "$f" ] || printf '%s\n' "$f"; done) |
  (cd "$repo" && tar -cf - -T -) | (cd "$fresh" && tar -xf -)

passed=0
failed=0
targets=$(sed -n 's/^\([a-z][a-z0-9-]*\):\([^=].*\)\{0,1\}$/\1/p' "$fresh/Makefile" | grep -vx bin | sort -u)
for target in $targets; do
  # bin/ as a path of this checkout: relative, or under its absolute path.
  first=$(cd "$fresh" && make -n "$target" 2>&1 | sed "s|$fresh/||g" |
    awk '/^mkdir -p bin$/ { print "made"; exit } /(^|[^\/A-Za-z0-9_.-])bin\// { print "named: " $0; exit }')
  case $first in
  named:*)
    failed=$((failed + 1))
    printf 'FAIL  make %s names bin/ before making it: %s\n' "$target" "${first#named: }"
    ;;
  *) passed=$((passed + 1)) ;;
  esac
done
printf 'fresh-bin: %s targets make bin/ before they name it, or never name it; %s don'"'"'t\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
