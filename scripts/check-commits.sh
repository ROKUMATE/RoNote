#!/bin/sh
# Checks the message of every commit in a revision range with the commit-msg
# hook in strict mode, so stored history follows the same rule as new commits.
#
#   check-commits.sh RANGE     e.g. main..HEAD
#
# Exits 0 when every commit passes, 1 when any is bad, 2 on a usage or internal
# error (unknown range, temp dir, a hook that fails other than by rejecting).
# The bad-commit report goes to stderr in one stream; success goes to stdout.
set -u

[ "$#" -eq 1 ] || {
  echo 'usage: check-commits.sh RANGE' >&2
  exit 2
}
range=$1
hook=$(cd "$(dirname "$0")/.." && pwd)/.githooks/commit-msg
work=$(mktemp -d "${TMPDIR:-/tmp}/check-commits.XXXXXX") || exit 2
trap 'rm -rf "$work"' EXIT

shas=$(git rev-list --reverse "$range") || exit 2
total=0
bad=0
for sha in $shas; do
  total=$((total + 1))
  git log -1 --format=%B "$sha" >"$work/msg" || exit 2
  COMMIT_MSG_STRICT=1 "$hook" "$work/msg" 2>"$work/err"
  status=$?
  case $status in
    0) ;;
    1)
      bad=$((bad + 1))
      printf 'BAD  %s\n' "$(git log -1 --format='%h %s' "$sha")" >&2
      sed 's/^/     /' "$work/err" >&2
      ;;
    *)
      cat "$work/err" >&2
      printf 'check-commits: %s exited %s on %s\n' "$hook" "$status" "$sha" >&2
      exit 2
      ;;
  esac
done

if [ "$bad" -gt 0 ]; then
  printf 'check-commits: %s of %s commits in %s are bad\n' "$bad" "$total" "$range" >&2
  exit 1
fi
printf 'check-commits: ok (%s commits in %s)\n' "$total" "$range"
