#!/bin/sh
# Runs scripts/prod-journals.sh against a fake id and a fake runner standing
# in for deploy/as-ronote.sh. Exits non-zero if any case fails.
set -u

scripts=$(cd "$(dirname "$0")" && pwd)
tool=$scripts/prod-journals.sh
work=$(mktemp -d "${TMPDIR:-/tmp}/prod-journals-test.XXXXXX")
trap 'rm -rf "$work"' EXIT
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

bin=$work/bin
mkdir -p "$bin"
# id ronote fails unless the ronote user exists (NO_USER unset).
cat >"$bin/id" <<'FAKE'
#!/bin/sh
[ -z "${NO_USER:-}" ]
FAKE
chmod +x "$bin/id"
# The runner logs its calls; UNITS are the ronote unit files systemd lists.
cat >"$work/runner.sh" <<'FAKE'
#!/bin/sh
printf '%s\n' "$*" >>"$CALLS"
case $* in
  *"list-unit-files"*) for u in $UNITS; do printf '%s enabled\n' "$u"; done ;;
  *journalctl*) echo "Nov 22 ronote-api[1]: request GET /healthz 200" ;;
  *"podman ps"*) echo "ronote-api Up 2 minutes (healthy)" ;;
esac
FAKE

CALLS=$work/calls
export CALLS

# run [VAR=VALUE...]: the log goes to $work/out/deeper/log, in a directory
# that doesn't exist; $out is what the script said, $log what it wrote.
run() {
  rm -rf "$work/out"
  : >"$CALLS"
  # shellcheck disable=SC2086 # one word per assignment
  out=$(env UNITS='' NO_USER='' "$@" PATH="$bin:$PATH" RONOTE_AS_RONOTE="$work/runner.sh" sh "$tool" "$work/out/deeper/log" 2>&1)
  status=$?
  log=$(cat "$work/out/deeper/log" 2>/dev/null)
  calls=$(cat "$CALLS")
}

run NO_USER=1
if [ "$status" -eq 0 ] && [ "$log" = "no units installed" ] && [ -z "$calls" ] && [ -z "$out" ]; then
  result 'no ronote user: one line, no error, nothing asked of ronote' ok
else
  result 'no ronote user: one line, no error, nothing asked of ronote' "exit $status; log '$log'; said '$out'; calls '$calls'"
fi

run UNITS=''
if [ "$status" -eq 0 ] && [ "$log" = "no units installed" ]; then
  result 'a ronote user with no units: the same one line' ok
else
  result 'a ronote user with no units: the same one line' "exit $status; log '$log'; said '$out'"
fi

run UNITS='ronote-api.service ronote-caddy.service'
case $status:$log in
  0:*"request GET /healthz 200"*"ronote-api Up 2 minutes (healthy)"*) result 'installed units: their journal, then their containers state' ok ;;
  *) result 'installed units: their journal, then their containers state' "exit $status; log '$log'" ;;
esac
case $log in *"no units installed"*) result 'installed units are not reported as none' "log '$log'" ;; *) result 'installed units are not reported as none' ok ;; esac

run UNITS='ronote-api.service'
case $calls in
  *"journalctl --user -b --no-pager -o short-precise -u ronote-*"*) result 'the journal is of ronote units since boot' ok ;;
  *) result 'the journal is of ronote units since boot' "calls '$calls'" ;;
esac

printf '%s passed, %s failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
