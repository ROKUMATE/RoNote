#!/bin/sh
# Runs scripts/run.sh and scripts/stop.sh on a throwaway job that starts a
# child of its own, with the groups recorded in a temporary directory.
# Exits non-zero if any case fails.
set -u

scripts=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/run-stop-test.XXXXXX")
RONOTE_RUN_DIR=$work
export RONOTE_RUN_DIR
passed=0
failed=0

result() {
  if [ "$2" = ok ]; then
    passed=$((passed + 1))
    printf 'ok    %s\n' "$1"
  else
    failed=$((failed + 1))
    printf 'FAIL  %s (%s)\n' "$1" "$2"
  fi
}

# start NAME: runs a job whose shell starts a sleeping child, and waits
# until run.sh has recorded its group. Sets wrapper and pgid.
start() {
  sh "$scripts/run.sh" "$1" sh -c 'sleep 60 & sleep 60' &
  wrapper=$!
  i=0
  while [ ! -s "$work/$1.pgid" ] && [ "$i" -lt 50 ]; do
    sleep 0.1
    i=$((i + 1))
  done
  pgid=$(cat "$work/$1.pgid" 2>/dev/null || echo 0)
}

# gone: whether the job's whole group has ended, child included.
gone() {
  i=0
  while kill -s 0 -- "-$pgid" 2>/dev/null && [ "$i" -lt 50 ]; do
    sleep 0.1
    i=$((i + 1))
  done
  ! kill -s 0 -- "-$pgid" 2>/dev/null
}

start stopped
if [ "$pgid" = 0 ] || ! kill -s 0 -- "-$pgid" 2>/dev/null; then
  result 'run.sh records a running group' 'no group recorded'
else
  result 'run.sh records a running group' ok
  out=$(sh "$scripts/stop.sh")
  wait "$wrapper"
  if gone && [ ! -e "$work/stopped.pgid" ] && [ "$out" = 'stopped stopped' ]; then
    result 'stop.sh ends the group, child included' ok
  else
    result 'stop.sh ends the group, child included' "output '$out'"
  fi
fi

start terminated
kill -s TERM "$wrapper"
wait "$wrapper"
if gone && [ ! -e "$work/terminated.pgid" ]; then
  result 'TERM to run.sh ends its group' ok
else
  result 'TERM to run.sh ends its group' 'group still running'
fi
kill -s KILL -- "-$pgid" 2>/dev/null

if [ -z "$(sh "$scripts/stop.sh")" ]; then
  result 'stop.sh with nothing running does nothing' ok
else
  result 'stop.sh with nothing running does nothing' 'it printed something'
fi

rm -rf "$work"
printf '%s passed, %s failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
