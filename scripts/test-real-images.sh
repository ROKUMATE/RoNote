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
  out=$(env NO_ARM64='' NO_AMD64='' SINGLE='' BROKEN='' KILLED='' RESTARTS='' "$@" PATH="$bin:$PATH" RONOTE_AS_RONOTE="$work/runner.sh" sh "$tool" "$sub" 2>"$work/err")
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
