#!/bin/sh
# Runs deploy/report-failed.sh with a fake runner standing in for
# deploy/as-ronote.sh: it records the commands it is given, and answers
# is-active from the variable INACTIVE. Exits non-zero if any case fails.
set -u

scripts=$(cd "$(dirname "$0")" && pwd)
report=$scripts/../deploy/report-failed.sh
work=$(mktemp -d "${TMPDIR:-/tmp}/report-failed-test.XXXXXX")
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

# The fake: every command is logged; is-active fails for the services in
# INACTIVE, the healthcheck fails with a reason, and podman logs fails for a
# container that never started.
cat >"$work/runner.sh" <<'FAKE'
#!/bin/sh
printf '%s\n' "$*" >>"$LOG"
case $* in
  *is-active*)
    for s in $INACTIVE; do
      case $* in *" $s") exit 3 ;; esac
    done
    exit 0 ;;
  *"podman healthcheck run"*) echo "weed shell: connection refused"; exit 1 ;;
  *"podman logs"*"ronote-never") echo "no container with name ronote-never" >&2; exit 125 ;;
  *) echo "output of: $*" ;;
esac
FAKE

LOG=$work/log
export LOG

# report INACTIVE SERVICE...: runs the report, leaving its output in $out
# and the commands the fake saw in $log.
report() {
  INACTIVE=$1
  export INACTIVE
  shift
  : >"$LOG"
  out=$(sh "$report" "$work/runner.sh" "$@" 2>&1)
  status=$?
  log=$(cat "$LOG")
}

# saw WHAT: the fake was given the command WHAT, whole.
saw() {
  printf '%s\n' "$log" | grep -qxF -- "$1"
}

report '' ronote-api.service ronote-caddy.service
if [ "$status" -ne 0 ]; then
  result 'active units are no failure' "exit $status"
elif printf '%s\n' "$log" | grep -qv 'is-active'; then
  result 'active units are only probed' "ran: $log"
else
  result 'active units are only probed' ok
fi

report ronote-seaweedfs.service ronote-api.service ronote-seaweedfs.service
for want in \
  'systemctl --user status --no-pager -l ronote-seaweedfs.service' \
  'journalctl --user -u ronote-seaweedfs.service -n 200 --no-pager' \
  'podman logs --tail 200 ronote-seaweedfs' \
  'podman inspect --format {{json .State.Health}} ronote-seaweedfs' \
  'podman healthcheck run ronote-seaweedfs'; do
  if saw "$want"; then
    result "an inactive unit gets: $want" ok
  else
    result "an inactive unit gets: $want" "not run; ran: $log"
  fi
done
case $out in
  *"weed shell: connection refused"*) result 'a failed healthcheck shows its output' ok ;;
  *) result 'a failed healthcheck shows its output' "got: $out" ;;
esac
case $out in
  *"exit 1"*) result 'a failed healthcheck shows its exit status' ok ;;
  *) result 'a failed healthcheck shows its exit status' "got: $out" ;;
esac
if printf '%s\n' "$log" | grep -v 'is-active' | grep -q 'ronote-api'; then
  result 'an active unit gets no report' "ran: $log"
else
  result 'an active unit gets no report' ok
fi
if [ "$status" -ne 0 ]; then
  result 'the report itself succeeds' "exit $status"
else
  result 'the report itself succeeds' ok
fi

report ronote-never.service ronote-never.service
case $out in
  *"no container with name ronote-never"*) result 'a missing container shows why' ok ;;
  *) result 'a missing container shows why' "got: $out" ;;
esac
if saw 'podman healthcheck run ronote-never'; then
  result 'a failing command does not stop the rest' ok
else
  result 'a failing command does not stop the rest' "ran: $log"
fi

printf '%s passed, %s failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
