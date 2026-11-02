#!/bin/sh
# Runs scripts/check-commits.sh and scripts/commit-range.sh against a throwaway
# repository. Exits non-zero if any case fails.
set -u

scripts=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/check-commits-test.XXXXXX")
trap 'rm -rf "$work"' EXIT
passed=0
failed=0

repo=$work/repo
git init -q -b main "$repo"

# git_ runs git in the test repo with a fixed identity and no hooks.
git_() {
  git -C "$repo" -c user.name=Test -c user.email=test@example.com -c core.hooksPath=/dev/null "$@"
}

# commit MESSAGE: records an empty commit with MESSAGE stored exactly as given
# and prints its short sha.
commit() {
  git_ commit -q --allow-empty --cleanup=verbatim -m "$1"
  git_ rev-parse --short HEAD
}

# expect NAME STATUS RANGE TEXT...: check-commits.sh on RANGE exits with
# STATUS and prints every TEXT.
expect() {
  name=$1
  want=$2
  range=$3
  shift 3
  out=$(cd "$repo" && sh "$scripts/check-commits.sh" "$range" 2>&1)
  status=$?
  missing=
  for text in "$@"; do
    case $out in
      *"$text"*) ;;
      *) missing="$missing [$text]" ;;
    esac
  done
  if [ "$status" -eq "$want" ] && [ -z "$missing" ]; then
    passed=$((passed + 1))
    printf 'ok    %s\n' "$name"
  else
    failed=$((failed + 1))
    printf 'FAIL  %s (exit %s, want %s; missing%s)\n' "$name" "$status" "$want" "${missing:- nothing}"
    printf '%s\n' "$out" | sed 's/^/      /'
  fi
}

root=$(commit 'Chore: Start')
commit 'Feat: Add page importer' >/dev/null
commit 'Fix: Retry slow fetches' >/dev/null
expect 'all good' 0 "$root..HEAD" 'ok (2 commits'

good=$(git_ rev-parse --short HEAD)
body=$(commit 'Feat: Add notes

They live in the right margin.')
commit 'Test: Cover notes' >/dev/null
trailer=$(commit 'Doc: Explain notes

Co-Authored-By: Claude <noreply@anthropic.com>')
expect 'bad commits are named' 1 "$good..HEAD" \
  "BAD  $body Feat: Add notes" \
  "BAD  $trailer Doc: Explain notes" \
  'extra line: Co-Authored-By' \
  '2 of 3 commits'

base=$(git_ rev-parse --short HEAD)
git_ checkout -q -b feature
commit 'Feat: Add tags' >/dev/null
git_ checkout -q main
git_ merge -q --no-ff -m "Merge branch 'feature'" feature
expect 'one-line git merge' 0 "$base..HEAD" 'ok (2 commits'

# range_is NAME WANT VAR=VALUE...: commit-range.sh run with only VARs set
# prints WANT.
range_is() {
  name=$1
  want=$2
  shift 2
  got=$(cd "$repo" && env -i PATH="$PATH" "$@" sh "$scripts/commit-range.sh" 2>&1)
  if [ "$got" = "$want" ]; then
    passed=$((passed + 1))
    printf 'ok    range: %s\n' "$name"
  else
    failed=$((failed + 1))
    printf 'FAIL  range: %s\n      got:  %s\n      want: %s\n' "$name" "$got" "$want"
  fi
}

zeros=0000000000000000000000000000000000000000
first=$(git_ rev-list --max-parents=0 HEAD)
head=$(git_ rev-parse HEAD)
prev=$(git_ rev-parse HEAD~2)
git_ checkout -q --orphan rewritten
commit 'Chore: Rewrite history' >/dev/null
unrelated=$(git_ rev-parse HEAD)
git_ checkout -q main

range_is 'pull request' "$prev..$head" PR_BASE_SHA="$prev" PR_HEAD_SHA="$head" PUSH_SHA="$head"
range_is 'push' "$prev..$head" PUSH_BEFORE_SHA="$prev" PUSH_SHA="$head"
range_is 'push of a new branch' "$head~1..$head" PUSH_BEFORE_SHA="$zeros" PUSH_SHA="$head"
range_is 'force push' "$head~1..$head" PUSH_BEFORE_SHA="$unrelated" PUSH_SHA="$head"
range_is 'push of a root commit' "$first" PUSH_BEFORE_SHA="$zeros" PUSH_SHA="$first"
range_is 'no event' 'commit-range: set PR_BASE_SHA and PR_HEAD_SHA, or PUSH_SHA'

printf '\n%s passed, %s failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
