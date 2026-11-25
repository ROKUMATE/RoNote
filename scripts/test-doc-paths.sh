#!/bin/sh
# Fails when a file names a docs/*.md path that doesn't exist: an ADR, the
# runbook or an agent reference renamed or never written. It reads the
# files git tracks or would track, so a new file is checked before it is
# added; testdata holds pages from elsewhere, and is skipped.
set -u

repo=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo" || exit 1
files() {
  git ls-files -co --exclude-standard | grep -v '^testdata/' |
    while read -r f; do [ ! -f "$f" ] || printf '%s\n' "$f"; done
}
named=$(files | xargs grep -Iho 'docs/[A-Za-z0-9_./-]*\.md' | sort -u)
missing=0
for path in $named; do
  if [ ! -f "$path" ]; then
    printf 'FAIL  %s is named, and does not exist:\n' "$path"
    files | xargs grep -Fn -- "$path" | sed 's/^/      /'
    missing=$((missing + 1))
  fi
done
printf 'doc-paths: %s docs paths named, %s missing\n' "$(printf '%s\n' "$named" | grep -c .)" "$missing"
[ "$missing" -eq 0 ]
