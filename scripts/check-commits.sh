#!/bin/sh
# Checks the message of every commit in a revision range with the commit-msg
# hook in strict mode, so stored history follows the same rule as new commits.
#
#   check-commits.sh RANGE     e.g. main..HEAD
set -u

[ "$#" -eq 1 ] || {
  echo 'usage: check-commits.sh RANGE' >&2
  exit 2
}
range=$1
hook=$(cd "$(dirname "$0")/.." && pwd)/.githooks/commit-msg
work=$(mktemp -d "${TMPDIR:-/tmp}/check-commits.XXXXXX")
trap 'rm -rf "$work"' EXIT

shas=$(git rev-list --reverse "$range") || exit 2
total=0
bad=0
for sha in $shas; do
  total=$((total + 1))
  git log -1 --format=%B "$sha" >"$work/msg"
  if ! COMMIT_MSG_STRICT=1 "$hook" "$work/msg" 2>"$work/err"; then
    bad=$((bad + 1))
    printf 'BAD  %s\n' "$(git log -1 --format='%h %s' "$sha")"
    sed 's/^/     /' "$work/err"
  fi
done

if [ "$bad" -gt 0 ]; then
  printf 'check-commits: %s of %s commits in %s are bad\n' "$bad" "$total" "$range" >&2
  exit 1
fi
printf 'check-commits: ok (%s commits in %s)\n' "$total" "$range"
