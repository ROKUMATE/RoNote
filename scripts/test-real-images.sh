#!/bin/sh
# Runs scripts/real-images.sh against a fake podman and a fake runner
# standing in for deploy/as-ronote.sh. Exits non-zero if any case fails.
set -u

scripts=$(cd "$(dirname "$0")" && pwd)
tool=$scripts/real-images.sh
work=$(mktemp -d "${TMPDIR:-/tmp}/real-images-test.XXXXXX")
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
# podman manifest inspect: the arch of each image is MANIFEST_ARCH, except
# those named in NO_ARM64, which only have amd64.
cat >"$bin/podman" <<'FAKE'
#!/bin/sh
case $* in
  "manifest inspect"*)
    for ref; do :; done
    arch=arm64
    case " $NO_ARM64 " in *" ${ref%%@*} "*) arch=amd64 ;; esac
    printf '{\n  "manifests": [\n    { "platform": { "architecture": "%s", "os": "linux" } }\n  ]\n}\n' "$arch" ;;
esac
FAKE
chmod +x "$bin/podman"

# The runner: one healthy unit set, then what each case changes through
# environment variables. UNITS lists the services; each container has a peak
# (bytes) in PEAK_<name>, a limit of 4 GiB, and no kill or restart unless
# KILLED or RESTARTS name it.
cat >"$work/runner.sh" <<'FAKE'
#!/bin/sh
case $* in
  *"list-units"*) for u in $UNITS; do printf '%s.service loaded active running RoNote\n' "$u"; done
                  printf 'ronote-edge-network.service loaded active exited net\n' ;;
  *"container exists"*) for last; do :; done; case $last in ronote-edge-network) exit 1 ;; esac ;;
  *"show -p MemoryPeak"*) for last; do :; done; name=${last%.service}
                          eval "printf '%s\n' \"\${PEAK_$(printf '%s' "$name" | tr - _)}\"" ;;
  *"show -p NRestarts"*) for last; do :; done; name=${last%.service}
                         case " $RESTARTS " in *" $name "*) echo 3 ;; *) echo 0 ;; esac ;;
  *"State.OOMKilled"*) for last; do :; done; case " $KILLED " in *" $last "*) echo true ;; *) echo false ;; esac ;;
  *"HostConfig.Memory"*) echo 4294967296 ;;
esac
FAKE

UNITS='ronote-docling ronote-api'
PEAK_ronote_docling=2147483648
PEAK_ronote_api=104857600
export UNITS PEAK_ronote_docling PEAK_ronote_api

# run SUBCOMMAND [VAR=VALUE...]: leaves stdout in $out, stderr in $err and
# the exit in $status.
run() {
  sub=$1
  shift
  # shellcheck disable=SC2086 # one word per assignment
  out=$(env NO_ARM64='' KILLED='' RESTARTS='' "$@" PATH="$bin:$PATH" RONOTE_AS_RONOTE="$work/runner.sh" sh "$tool" "$sub" 2>"$work/err")
  status=$?
  err=$(cat "$work/err")
}

run arm64
if [ "$status" -eq 0 ] && [ -z "$err" ]; then result 'every image with an arm64 build passes' ok; else result 'every image with an arm64 build passes' "exit $status: $err"; fi
case $out in
  *"ollama"*"| yes |"*) result 'the table lists the images' ok ;;
  *) result 'the table lists the images' "said: $out" ;;
esac

run arm64 NO_ARM64=quay.io/docling-project/docling-serve-cpu:v1.38.2
case $status:$err:$out in
  1:*"no linux/arm64 build of quay.io/docling-project/docling-serve-cpu:v1.38.2"*:*"Options"*) result 'an image with no arm64 build fails, with options' ok ;;
  *) result 'an image with no arm64 build fails, with options' "exit $status; $err; $out" ;;
esac

run memory
if [ "$status" -eq 0 ] && [ -z "$err" ]; then result 'containers within their limits pass' ok; else result 'containers within their limits pass' "exit $status: $err"; fi
case $out in
  *"| ronote-docling | 2048 MiB | 4096 MiB | 50% | ok |"*"| ronote-api | 100 MiB | 4096 MiB | 2% | ok |"*) result 'the table gives peak, limit and share' ok ;;
  *) result 'the table gives peak, limit and share' "said: $out" ;;
esac
case $out in *"edge-network"*) result 'only containers are listed' "said: $out" ;; *) result 'only containers are listed' ok ;; esac

run memory KILLED=ronote-docling PEAK_ronote_docling=4294967296
case $status:$err:$out in
  1:*"ronote-docling was killed for memory at its 4096 MiB limit"*:*"**OOM-killed**"*"Options"*"4 OCPUs and 24 GB"*) result 'an OOM-killed docling fails, with options' ok ;;
  *) result 'an OOM-killed docling fails, with options' "exit $status; $err; $out" ;;
esac

run memory RESTARTS=ronote-api
case $status:$err in
  1:*"ronote-api restarted 3 times"*) result 'a container that restarted fails' ok ;;
  *) result 'a container that restarted fails' "exit $status: $err" ;;
esac

run memory PEAK_ronote_docling=4000000000
case $status:$out in
  0:*"near its limit"*) result 'near its limit is flagged, not failed' ok ;;
  *) result 'near its limit is flagged, not failed' "exit $status: $out" ;;
esac

printf '%s passed, %s failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
