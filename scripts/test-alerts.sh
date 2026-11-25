#!/bin/sh
# Runs deploy/alerts/watch.sh and alert.sh against fake date, df, systemctl,
# podman, openssl, journalctl and curl, which answer from variables the test
# sets; curl logs what it was asked to send. The ntfy topic must never be in
# an argument or in what the scripts say. Exits non-zero if any case fails.
set -u

scripts=$(cd "$(dirname "$0")" && pwd)
alerts=$scripts/../deploy/alerts
work=$(mktemp -d "${TMPDIR:-/tmp}/alerts-test.XXXXXX")
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

TOPIC=$(od -An -N8 -tx1 /dev/urandom | tr -d ' \n')
export TOPIC
bin=$work/bin
mkdir -p "$bin"
# fake NAME: a command on the test's PATH, its script on stdin.
fake() {
  { printf '#!/bin/sh\n'; cat; } >"$bin/$1"
  chmod +x "$bin/$1"
}
fake date <<'FAKE'
echo "$NOW"
FAKE
fake df <<'FAKE'
printf 'Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/sda1 100 50 50 %s%% /\n' "$DISK"
FAKE
fake systemctl <<'FAKE'
for s in $DOWN; do
  case $* in *" $s.service") exit 3 ;; esac
done
FAKE
fake podman <<'FAKE'
case $* in
  "secret inspect"*) echo "$TOPIC" ;;
  inspect*) case $* in *" $UNHEALTHY") echo unhealthy ;; *) echo healthy ;; esac ;;
esac
FAKE
fake openssl <<'FAKE'
case $1 in
  x509) [ -z "$EXPIRING" ] ;;
  *) cat >/dev/null ;;
esac
FAKE
fake journalctl <<'FAKE'
printf '%s\n' "$API_LOG"
FAKE
fake curl <<'FAKE'
printf '%s\n' "$*" >>"$SENT"
cat >>"$SENT.config"
FAKE

home=$work/home
mkdir -p "$home/.config/ronote" "$home/.config/containers/systemd"
: >"$home/.config/containers/systemd/ronote-api.container"
: >"$home/.config/containers/systemd/ronote-caddy.container"
printf 'RONOTE_PUBLIC_URL=https://ronote.example.me\nRONOTE_NTFY_URL=https://ntfy.example\n' >"$home/.config/ronote/ronote.env"

SENT=$work/sent
NOW=1000000 DISK=50 DOWN='' UNHEALTHY=none EXPIRING='' API_LOG=''
export SENT NOW DISK DOWN UNHEALTHY EXPIRING API_LOG

# watch [VAR=VALUE...]: one run with those set, leaving the alerts it sent
# in $sent and its output in $out. (Assignments before a function call may
# outlast it, so they are arguments.)
watch() {
  : >"$SENT"
  out=$(env "$@" HOME="$home" PATH="$bin:$PATH" sh "$alerts/watch.sh" 2>&1)
  status=$?
  sent=$(cat "$SENT")
}

# alerts N NAME: the run sent N alerts.
alerts() {
  got=$(printf '%s' "$sent" | grep -c . || true)
  if [ "$status" -eq 0 ] && [ "$got" -eq "$1" ]; then
    result "$2" ok
  else
    result "$2" "exit $status, $got alerts: $sent; said: $out"
  fi
}

watch
alerts 0 'all well, no alert'

watch DISK=85
alerts 1 'a full disk alerts at once'
case $sent in *"Title: RoNote: the disk is 85% full"*) result 'the alert says how full' ok ;; *) result 'the alert says how full' "sent: $sent" ;; esac
watch DISK=85 NOW=1003600
alerts 0 'it does not repeat within the day'
watch DISK=85 NOW=1086500
alerts 1 'it repeats a day later while it lasts'
watch
alerts 0 'a disk freed again is quiet'
watch DISK=90
alerts 1 'and alerts afresh when it fills again'
watch

watch DOWN=ronote-api
alerts 0 'a unit down for less than 5 minutes is no alert yet'
watch DOWN=ronote-api NOW=1000301
alerts 1 'a unit down for 5 minutes alerts'
case $sent in *"ronote-api is down"*) result 'naming the unit' ok ;; *) result 'naming the unit' "sent: $sent" ;; esac
watch NOW=1000302

watch UNHEALTHY=ronote-caddy
watch UNHEALTHY=ronote-caddy NOW=1000400
alerts 1 'an unhealthy container for 5 minutes alerts'
watch NOW=1000401

watch EXPIRING=1
alerts 1 'a certificate within 14 days of expiring alerts'
case $sent in *"ronote.example.me's certificate"*) result 'naming the app' ok ;; *) result 'naming the app' "sent: $sent" ;; esac
watch

watch API_LOG='{"level":"WARN","msg":"chat spend cap reached","day":"2026-11-21","cap_usd":0.2}'
alerts 1 'the spend cap in the api log alerts'

watch API_LOG='{"level":"ERROR","msg":"embedders serve different models","index":"http://ronote-ollama:11434","query":"http://ronote-ollama-query:11434"}'
alerts 1 "the embedders' mismatch in the api log alerts"

leaked=false
for text in "$(cat "$SENT")" "$out"; do
  case $text in *"$TOPIC"*) leaked=true ;; esac
done
if [ "$leaked" = false ] && grep -q "url = \"https://ntfy.example/$TOPIC\"" "$SENT.config"; then
  result 'the topic reaches curl only on stdin' ok
else
  result 'the topic reaches curl only on stdin' "leaked $leaked; config: $(cat "$SENT.config")"
fi

printf 'RONOTE_PUBLIC_URL=https://ronote.example.me\n' >"$home/.config/ronote/ronote.env"
: >"$SENT"
out=$(HOME=$home PATH="$bin:$PATH" sh "$alerts/alert.sh" 'a test' 'nothing' 2>&1)
case $?:$out:$(cat "$SENT") in
  "0:alert (not sent, RONOTE_NTFY_URL is unset): a test: nothing:") result 'without RONOTE_NTFY_URL nothing is sent, and it says so' ok ;;
  *) result 'without RONOTE_NTFY_URL nothing is sent, and it says so' "said: $out" ;;
esac

printf '%s passed, %s failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
