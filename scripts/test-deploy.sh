#!/bin/sh
# Runs deploy/ronote-deploy.sh as bootstrap.sh installs it, beside a fake
# as-ronote.sh, with fake git, id and getent: git's archive is a release
# whose install.sh only logs how it was run. HEALTH is the version the api's
# /healthz says, INSTALL_FAILS a tag whose install fails. Exits non-zero if
# any case fails.
set -u

scripts=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/deploy-test.XXXXXX")
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

lib=$work/lib
bin=$work/bin
release=$work/release
mkdir -p "$lib" "$bin" "$release/deploy" "$release/scripts"
cp "$scripts/../deploy/ronote-deploy.sh" "$lib/"
: >"$release/scripts/embed-check.sh"
cat >"$release/deploy/install.sh" <<'FAKE'
#!/bin/sh
printf 'install %s\n' "$*" >>"$LOG"
case " $* " in *" --version $INSTALL_FAILS "*) exit 1 ;; esac
FAKE
cat >"$lib/as-ronote.sh" <<'FAKE'
#!/bin/sh
case $* in
  "podman pull"*) ;;
  "podman image inspect"*) for last; do :; done; printf 'sha256:%s\n' "$(printf '%s' "$last" | cksum | cut -d' ' -f1)" ;;
  "podman run"*" migrate status") echo "schema at version 19; this build's newest is 19" ;;
  "podman run"*" migrate") ;;
  "podman exec ronote-caddy wget"*) printf '{"status":"ok","version":"%s"}\n' "$HEALTH" ;;
  "sh "*alert.sh*) shift 2; printf 'alert %s: %s\n' "$1" "$2" >>"$LOG" ;;
  *) echo "as-ronote: unexpected $*" >&2; exit 9 ;;
esac
FAKE
cat >"$bin/git" <<'FAKE'
#!/bin/sh
case $* in
  *fetch*) ;;
  *archive*) tar -cf - -C "$RELEASE" deploy scripts ;;
esac
FAKE
cat >"$bin/id" <<'FAKE'
#!/bin/sh
echo 0
FAKE
cat >"$bin/getent" <<'FAKE'
#!/bin/sh
echo "ronote:x:1001:1001::/home/ronote:/usr/sbin/nologin"
FAKE
chmod +x "$bin"/*

LOG=$work/log
STATE=$work/state
RELEASE=$release
export LOG RELEASE

# deploy [VAR=VALUE...] [TAG]: one deploy with those set, leaving its output
# in $out, its exit in $status, and what it ran in $log.
deploy() {
  : >"$LOG"
  vars="HEALTH= INSTALL_FAILS=none"
  while :; do
    case ${1:-} in
      *=*) vars="$vars $1"; shift ;;
      *) break ;;
    esac
  done
  # shellcheck disable=SC2086 # one word per assignment
  out=$(env $vars PATH="$bin:$PATH" RONOTE_DEPLOY_STATE="$STATE" sh "$lib/ronote-deploy.sh" "$@" 2>&1)
  status=$?
  log=$(cat "$LOG")
}

current() {
  sed -n 's/^tag=//p' "$STATE/current" 2>/dev/null
}

for tag in v1.2 'v1.2.3; rm -rf /' ''; do
  deploy "$tag"
  if [ "$status" -eq 2 ] && [ -z "$log" ]; then
    result "the tag '$tag' is refused, running nothing" ok
  else
    result "the tag '$tag' is refused, running nothing" "exit $status; ran: $log"
  fi
done

deploy HEALTH=v1.0.0 v1.0.0
if [ "$status" -eq 0 ] && [ "$(current)" = v1.0.0 ] && grep -q 'pins=--pin ronote-api=ghcr.io/rokumate/ronote-api:v1.0.0@sha256:' "$STATE/current"; then
  result 'a healthy release is recorded, pinned by digest' ok
else
  result 'a healthy release is recorded, pinned by digest' "exit $status: $out"
fi
case $log in
  *"install --version v1.0.0 --pin ronote-api=ghcr.io/rokumate/ronote-api:v1.0.0@sha256:"*"--pin ronote-worker="*"--pin ronote-web="*) result 'its install pins all three images' ok ;;
  *) result 'its install pins all three images' "ran: $log" ;;
esac

# The api still says the old version: the new one never took over.
deploy HEALTH=v1.0.0 v1.1.0
if [ "$status" -ne 0 ] && [ "$(current)" = v1.0.0 ]; then
  result 'a release the health check refuses is not recorded' ok
else
  result 'a release the health check refuses is not recorded' "exit $status, current $(current): $out"
fi
case $log in
  *"install --version v1.1.0"*"install --version v1.0.0 --pin ronote-api=ghcr.io/rokumate/ronote-api:v1.0.0@sha256:"*"alert deploying v1.1.0 failed: the api runs another version"*"v1.0.0 runs again"*)
    result 'it goes back to the previous digests, and alerts' ok ;;
  *) result 'it goes back to the previous digests, and alerts' "ran: $log" ;;
esac

deploy HEALTH=v1.1.0 INSTALL_FAILS=v1.1.0 v1.1.0
case $status:$log in
  0:*) result 'units that fail to start send it back too' "passed: $out" ;;
  *"install --version v1.0.0"*"alert deploying v1.1.0 failed: the units didn't start"*) result 'units that fail to start send it back too' ok ;;
  *) result 'units that fail to start send it back too' "exit $status; ran: $log" ;;
esac

deploy HEALTH=v1.1.0 SSH_ORIGINAL_COMMAND=v1.1.0
if [ "$status" -eq 0 ] && [ "$(current)" = v1.1.0 ] && [ "$(sed -n 's/^tag=//p' "$STATE/previous")" = v1.0.0 ]; then
  result 'the forced command takes the tag the SSH client sent' ok
else
  result 'the forced command takes the tag the SSH client sent' "exit $status: $out"
fi

mkdir "$STATE/lock"
deploy HEALTH=v1.2.0 v1.2.0
case $status:$out in
  1:*"another deploy is running"*) result 'one deploy at a time' ok ;;
  *) result 'one deploy at a time' "exit $status: $out" ;;
esac
rmdir "$STATE/lock"

printf '%s passed, %s failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
