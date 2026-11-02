#!/bin/sh
# Prints the revision range of commits a CI run should check.
#
#   pull_request:  PR_BASE_SHA..PR_HEAD_SHA
#   push:          PUSH_BEFORE_SHA..PUSH_SHA, or PUSH_SHA~1..PUSH_SHA when
#                  before is all zeros (a new branch) or not an ancestor of
#                  PUSH_SHA (a force push); just PUSH_SHA for a root commit.
set -u

if [ -n "${PR_BASE_SHA:-}" ]; then
  printf '%s..%s\n' "$PR_BASE_SHA" "${PR_HEAD_SHA:?PR_HEAD_SHA is not set}"
  exit 0
fi

sha=${PUSH_SHA:-}
before=${PUSH_BEFORE_SHA:-}
[ -n "$sha" ] || {
  echo 'commit-range: set PR_BASE_SHA and PR_HEAD_SHA, or PUSH_SHA' >&2
  exit 2
}

# usable_before succeeds when before names a real ancestor of sha.
usable_before() {
  case $before in
    *[!0]*) git merge-base --is-ancestor "$before" "$sha" 2>/dev/null ;;
    *) return 1 ;;
  esac
}

if usable_before; then
  printf '%s..%s\n' "$before" "$sha"
elif git rev-parse --verify --quiet "$sha~1" >/dev/null; then
  printf '%s~1..%s\n' "$sha" "$sha"
else
  printf '%s\n' "$sha"
fi
