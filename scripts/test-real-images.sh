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
# podman manifest inspect, as podman answers it: a reference with a tag and
# a digest is refused; an image named in BROKEN can't be read; one in SINGLE
# is a single-arch manifest, not an index; one in NO_ARM64 or NO_AMD64 is an
# index without that platform (the others have both). Names are without tag.
cat >"$bin/podman" <<'FAKE'
#!/bin/sh
case $* in
  "manifest inspect"*)
    for ref; do :; done
    case $ref in
      *:*@*) echo "Error: getting content of manifest list or image $ref: Docker references with both a tag and digest are currently not supported" >&2; exit 125 ;;
    esac
    base=${ref%%@*}
    case " $BROKEN " in *" $base "*) echo "Error: pinging container registry registry-1.docker.io: connection refused" >&2; exit 125 ;; esac
    case " $SINGLE " in *" $base "*)
      printf '{\n  "schemaVersion": 2,\n  "mediaType": "application/vnd.oci.image.manifest.v1+json",\n  "config": { "digest": "sha256:aa" },\n  "layers": []\n}\n'
      exit 0 ;;
    esac
    printf '{\n  "schemaVersion": 2,\n  "mediaType": "application/vnd.oci.image.index.v1+json",\n  "manifests": [\n'
    case " $NO_AMD64 " in *" $base "*) ;; *) printf '    { "digest": "sha256:01", "platform": { "architecture": "amd64", "os": "linux" } },\n' ;; esac
    printf '    { "digest": "sha256:02", "platform": { "architecture": "unknown", "os": "unknown" } }'
    case " $NO_ARM64 " in *" $base "*) ;; *) printf ',\n    { "digest": "sha256:03", "platform": { "architecture": "arm64", "os": "linux", "variant": "v8" } }' ;; esac
    printf '\n  ]\n}\n' ;;
esac
FAKE
chmod +x "$bin/podman"

# The runner: the services UNITS names, none restarted unless RESTARTS names
# it; podman inspect knows two containers a script ran, by their IDs, and
# logs each call to INSPECTED, as convert-check does, which converts any PDF
# but one saying timeout or broken.
cat >"$work/runner.sh" <<'FAKE'
#!/bin/sh
case $* in
  *"list-units"*) for u in $UNITS; do printf '%s.service loaded active running RoNote\n' "$u"; done
                  printf 'ronote-edge-network.service loaded active exited net\n' ;;
  *"show -p NRestarts"*) for last; do :; done; name=${last%.service}
                         case " $RESTARTS " in *" $name "*) echo 3 ;; *) echo 0 ;; esac ;;
  *"podman inspect"*) for last; do :; done; echo "$last" >>"$INSPECTED"
                      case $last in
                        aaaa) echo 'quirky_turing docker.io/restic/restic:0.18.1@sha256:0a' ;;
                        bbbb) echo 'ronote-drill-postgres docker.io/pgvector/pgvector:pg17@sha256:0b' ;;
                        *) echo "Error: no such object: $last" >&2; exit 125 ;;
                      esac ;;
  *"convert-check"*) printf '%s\n' "$*" >>"$INSPECTED"
                     case $(cat) in
                       *timeout*) echo 'worker: convert-check: docling: pdf_conversion_timeout: document timeout exceeded' >&2; exit 1 ;;
                       *broken*) echo 'worker: convert-check: docling: unavailable: docling submit answered 502' >&2; exit 1 ;;
                       *) echo 'convert-check: ok' ;;
                     esac ;;
  *"system df"*) printf 'TYPE  TOTAL  ACTIVE  SIZE  RECLAIMABLE\nImages  9  9  7.1GB  0B (0%%)\n' ;;
esac
FAKE

UNITS='ronote-docling ronote-api'
INSPECTED=$work/inspected
export UNITS INSPECTED

# run 'SUBCOMMAND [ARG...]' [VAR=VALUE...]: leaves stdout in $out, stderr in $err and
# the exit in $status.
run() {
  sub=$1
  shift
  # shellcheck disable=SC2086 # one word per assignment
  # shellcheck disable=SC2086 # the subcommand and its arguments
  out=$(env NO_ARM64='' NO_AMD64='' SINGLE='' BROKEN='' RESTARTS='' "$@" PATH="$bin:$PATH" RONOTE_AS_RONOTE="$work/runner.sh" sh "$tool" $sub 2>"$work/err")
  status=$?
  err=$(cat "$work/err")
}

run arm64
if [ "$status" -eq 0 ] && [ -z "$err" ]; then result 'every image with an arm64 build passes' ok; else result 'every image with an arm64 build passes' "exit $status: $err"; fi
case $out in
  *"ollama"*"| yes |"*) result 'the table lists the images' ok ;;
  *) result 'the table lists the images' "said: $out" ;;
esac

# What podman is asked: the pin by its digest alone, never with its tag.
case $out in
  *"docker.io/ollama/ollama:0.40.16@sha256:"*"| yes | yes | yes |"*) result 'a good index passes, named with its tag and digest' ok ;;
  *) result 'a good index passes, named with its tag and digest' "said: $out" ;;
esac

run arm64 NO_ARM64=docker.io/ollama/ollama
case $status:$err:$out in
  1:*"no linux/arm64 entry in the index of docker.io/ollama/ollama:0.40.16@sha256:"*:*"Options"*) result 'an index without arm64 fails, with options' ok ;;
  *) result 'an index without arm64 fails, with options' "exit $status; $err; $out" ;;
esac

run arm64 NO_AMD64=quay.io/docling-project/docling-serve-cpu
case $status:$err in
  1:*"no linux/amd64 entry in the index of quay.io/docling-project/docling-serve-cpu"*) result 'an index without amd64 fails too' ok ;;
  *) result 'an index without amd64 fails too' "exit $status; $err" ;;
esac

run arm64 SINGLE=quay.io/docling-project/docling-serve-cpu
case $status:$err in
  1:*"quay.io/docling-project/docling-serve-cpu:v1.38.2@sha256:"*"single-arch manifest, not a multi-arch index"*) result 'a pin that is a single-arch manifest fails' ok ;;
  *) result 'a pin that is a single-arch manifest fails' "exit $status; $err" ;;
esac

# An error is reported as the error it is, never as a missing build.
run arm64 BROKEN=docker.io/ollama/ollama
case $status:$err in
  1:*"can't inspect docker.io/ollama/ollama:0.40.16@sha256:"*"pinging container registry registry-1.docker.io: connection refused"*) result 'an inspect error is the reason, in podman words' ok ;;
  *) result 'an inspect error is the reason, in podman words' "exit $status; $err" ;;
esac
case $err in
  *"no linux/arm64"*) result 'an inspect error is not called a missing arm64 build' "said: $err" ;;
  *) result 'an inspect error is not called a missing arm64 build' ok ;;
esac

# samples LINE...: a samples file of KEY PEAK ANON FILE LIMIT OOM-KILLS SWAP lines:
# docling's two samples, 2048 MiB peak, 1200 anon, 800 file of 4096; the api
# within 1 GiB; a backup's restic in its 512 MiB; and a network's cgroup.
samples() {
  {
    printf 'ronote-docling 1073741824 805306368 268435456 4294967296 0 0\n'
    printf 'ronote-docling 2147483648 1258291200 838860800 4294967296 0 0\n'
    printf 'ronote-api 104857600 52428800 10485760 1073741824 0 0\n'
    printf 'restic 50331648 41943040 1048576 536870912 0 0\n'
    printf 'ronote-edge-network 4096 4096 0 0 0 0\n'
    for line; do printf '%s\n' "$line"; done
  } >"$work/samples"
}

samples
run "memory $work/samples"
if [ "$status" -eq 0 ] && [ -z "$err" ]; then result 'containers within their limits pass' ok; else result 'containers within their limits pass' "exit $status: $err"; fi
case $out in
  *"| ronote-api | 100 MiB | 50 MiB | 10 MiB | 0 MiB | 1024 MiB | 9% | 4% | ok |"*"| ronote-docling | 2048 MiB | 1200 MiB | 800 MiB | 0 MiB | 4096 MiB | 50% | 29% | ok |"*) result 'the table splits the peak into anon and file' ok ;;
  *) result 'the table splits the peak into anon and file' "said: $out" ;;
esac
case $out in
  *"| restic | 48 MiB | 40 MiB | 1 MiB | 0 MiB | 512 MiB | 9% | 7% | ok |"*) result "a backup's container has its row" ok ;;
  *) result "a backup's container has its row" "said: $out" ;;
esac
case $out in *"edge-network"*) result 'only containers are listed' "said: $out" ;; *) result 'only containers are listed' ok ;; esac
case $out in
  *"ronote's units ran on CPUs unknown"*) result "a slice it can't read says so" ok ;;
  *) result "a slice it can't read says so" "said: $out" ;;
esac
mkdir -p "$work/slice/user@1001.service"
printf '0-1\n' >"$work/slice/cpuset.cpus.effective"
run "memory $work/samples" RONOTE_USER_CGROUP="$work/slice/user@1001.service"
case $out in
  *"ronote's units ran on CPUs 0-1 (its slice's cpuset.cpus.effective)."*) result "the report says which CPUs ronote's slice had" ok ;;
  *) result "the report says which CPUs ronote's slice had" "said: $out" ;;
esac
case $out in
  *'```'*"Images  9  9  7.1GB"*'```'*) result "podman system df follows, as ronote" ok ;;
  *) result "podman system df follows, as ronote" "said: $out" ;;
esac

samples 'ronote-docling 4294967296 4194304000 0 4294967296 1 0'
run "memory $work/samples"
case $status:$err:$out in
  1:*"ronote-docling was killed for memory at its 4096 MiB limit"*:*"**OOM-killed**"*"Options"*"Always Free's A1 allowance is 2 OCPUs and 12 GB"*) result 'an OOM-killed docling fails, with options' ok ;;
  *) result 'an OOM-killed docling fails, with options' "exit $status; $err; $out" ;;
esac
case $out in
  *"24 GB"* | *"4 OCPUs"*) result 'the options offer only what Always Free gives' "said: $out" ;;
  *) result 'the options offer only what Always Free gives' ok ;;
esac

samples
run "memory $work/samples" RESTARTS=ronote-api
case $status:$err in
  1:*"ronote-api restarted 3 times"*) result 'a unit that restarted fails' ok ;;
  *) result 'a unit that restarted fails' "exit $status: $err" ;;
esac

samples 'ronote-docling 3600000000 3500000000 100000000 4294967296 0 0'
run "memory $work/samples"
case $status:$out in
  0:*"| 81% | anon near its limit |"*) result 'anon over 80% is flagged, not failed' ok ;;
  *) result 'anon over 80% is flagged, not failed' "exit $status: $out" ;;
esac

samples 'ronote-docling 4000000000 2000000000 2000000000 4294967296 0 0'
run "memory $work/samples"
case $out in
  *"| 93% | 46% | ok |"*) result 'a peak of file cache alone is no warning' ok ;;
  *) result 'a peak of file cache alone is no warning' "said: $out" ;;
esac

# At its limit, with what didn't fit in swap: flagged, not failed.
samples 'ronote-docling 4294967296 4199546880 7340032 4294967296 0 1073741824'
run "memory $work/samples"
case $status:$out in
  0:*"| ronote-docling | 4096 MiB | 4005 MiB | 800 MiB | 1024 MiB | 4096 MiB | 100% | 97% | **swapped** |"*) result 'a container that swapped is flagged, with how much' ok ;;
  *) result 'a container that swapped is flagged, with how much' "exit $status: $out" ;;
esac

samples
# A stand-in sampler, started by another shell so this one reports nothing of it.
sampler=$(sh -c 'sleep 600 >/dev/null 2>&1 & echo $!')
echo "$sampler" >"$work/samples.pid"
run "memory $work/samples"
if ! kill -0 "$sampler" 2>/dev/null && [ ! -e "$work/samples.pid" ]; then
  result 'the report stops the sampler' ok
else
  kill "$sampler" 2>/dev/null
  result 'the report stops the sampler' 'still running'
fi

: >"$work/samples"
run "memory $work/samples"
case $status:$err in
  1:*"no samples in $work/samples"*) result 'no samples fails' ok ;;
  *) result 'no samples fails' "exit $status: $err" ;;
esac

# The sampler, once, on a cgroup tree: a unit, whose service's cgroup also
# counts podman's start, with its container's own; a unit between
# containers, with none; a backup's container in its scope with its conmon
# beside it; and a scope whose container has gone.
cg=$work/cg
mkdir -p "$cg/app.slice/ronote-api.service/libpod-payload-0123" "$cg/app.slice/ronote-docling.service" "$cg/user.slice/libpod-aaaa.scope" \
  "$cg/user.slice/libpod-conmon-aaaa.scope" "$cg/user.slice/libpod-cccc.scope"
api=$cg/app.slice/ronote-api.service
printf '9999\n' >"$api/memory.peak"
printf 'anon 9999\nfile 9999\n' >"$api/memory.stat"
printf 'max\n' >"$api/memory.max"
printf '1000\n' >"$api/libpod-payload-0123/memory.peak"
printf 'anon 600\nfile 300\nkernel 50\n' >"$api/libpod-payload-0123/memory.stat"
printf 'low 0\noom 0\noom_kill 0\n' >"$api/libpod-payload-0123/memory.events"
printf '2147483648\n' >"$api/libpod-payload-0123/memory.max"
printf '200\n' >"$api/libpod-payload-0123/memory.swap.peak"
printf '100\n' >"$api/libpod-payload-0123/memory.swap.current"
for d in libpod-aaaa.scope libpod-cccc.scope; do
  printf '5000\n' >"$cg/user.slice/$d/memory.peak"
  printf 'anon 4000\nfile 100\n' >"$cg/user.slice/$d/memory.stat"
  printf 'oom_kill 2\n' >"$cg/user.slice/$d/memory.events"
  printf '536870912\n' >"$cg/user.slice/$d/memory.max"
done
rm -f "$work/s" "$work/s.names" "$INSPECTED"
run "sample $work/s 0" RONOTE_USER_CGROUP="$cg"
run "sample $work/s 0" RONOTE_USER_CGROUP="$cg"
want='restic 5000 4000 100 536870912 2 0
ronote-api 1000 600 300 2147483648 0 200'
if [ "$status" -eq 0 ] && [ "$(sort -u "$work/s")" = "$want" ] && [ "$(sort "$work/s" | uniq -c | awk '{ print $1 }' | sort -u)" = 2 ]; then
  result "each sample has each container's peak, anon, file, limit, OOM kills and swap" ok
else
  result "each sample has each container's peak, anon, file, limit, OOM kills and swap" "exit $status $err: $(cat "$work/s")"
fi
if [ "$(grep -c aaaa "$INSPECTED")" = 1 ] && [ "$(cat "$work/s.names")" = 'aaaa restic' ]; then
  result "a script's container is named once, by its image" ok
else
  result "a script's container is named once, by its image" "inspected: $(cat "$INSPECTED"); names: $(cat "$work/s.names")"
fi

# The curve: docling's own cgroup, with what it holds while it converts.
dcg=$work/curve-cg
dl=$dcg/app.slice/ronote-docling.service/libpod-payload-0456
mkdir -p "$dl"
printf 'anon 1073741824\nfile 4096\n' >"$dl/memory.stat"
printf '268435456\n' >"$dl/memory.swap.current"
printf '4294967296\n' >"$dl/memory.max"
for n in 30 60 120; do printf '%%PDF-1.7 %s pages\n' "$n" >"$work/curve-$n.pdf"; done
rm -f "$INSPECTED"
run "curve 30 $work/curve-30.pdf 60 $work/curve-60.pdf 120 $work/curve-120.pdf" RONOTE_USER_CGROUP="$dcg"
case $status:$out in
  0:*"| 30 | 0 s | 0% | 1024 MiB | 25% | 256 MiB | ok |"*"| 60 |"*"| ok |"*"| 120 |"*"| ok |"*) result 'each size converts once, with its time, anon and swap' ok ;;
  *) result 'each size converts once, with its time, anon and swap' "exit $status $err: $out" ;;
esac
if [ "$(grep -c '^podman exec -i ronote-worker /ronote convert-check$' "$INSPECTED")" = 3 ]; then
  result "through the worker's own client, in its container" ok
else
  result "through the worker's own client, in its container" "calls: $(cat "$INSPECTED")"
fi

printf '%%PDF-1.7 timeout\n' >"$work/curve-60.pdf"
rm -f "$INSPECTED"
run "curve 30 $work/curve-30.pdf 60 $work/curve-60.pdf 120 $work/curve-120.pdf" RONOTE_USER_CGROUP="$dcg"
case $status:$out in
  0:*"| 60 |"*"| **timed out** |"*"| 120 | - |"*"skipped: 60 pages ran into the timeout"*) result 'a timeout is a result, and the larger sizes are skipped' ok ;;
  *) result 'a timeout is a result, and the larger sizes are skipped' "exit $status $err: $out" ;;
esac
if [ "$(grep -c convert-check "$INSPECTED")" = 2 ]; then result 'nothing larger is sent' ok; else result 'nothing larger is sent' "calls: $(cat "$INSPECTED")"; fi

printf '%%PDF-1.7 broken\n' >"$work/curve-30.pdf"
run "curve 30 $work/curve-30.pdf" RONOTE_USER_CGROUP="$dcg"
case $status:$err in
  1:*"docling failed on 30 pages"*"docling submit answered 502"*) result 'any other failure fails, with its reason' ok ;;
  *) result 'any other failure fails, with its reason' "exit $status: $err" ;;
esac

run "curve 30 $work/curve-30.pdf" RONOTE_USER_CGROUP="$work/nowhere"
case $status:$err in
  1:*"no docling container"*) result 'no docling to measure fails' ok ;;
  *) result 'no docling to measure fails' "exit $status: $err" ;;
esac

printf '%s passed, %s failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
