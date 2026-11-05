#!/bin/sh
# Runs "scripts/container.sh clock" against fake podman, docker and date
# commands, each clock set by the test. Exits non-zero if any case fails.
set -u

scripts=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/container-clock-test.XXXXXX")
trap 'rm -rf "$work"' EXIT
passed=0
failed=0

# The fakes: date prints HOST_TIME; podman answers as a running machine
# whose clock reads VM_TIME, unless RUNTIME is docker; docker answers
# "info" and runs a container whose clock reads VM_TIME.
bin=$work/bin
mkdir -p "$bin"
cat >"$bin/date" <<'EOF'
#!/bin/sh
echo "$HOST_TIME"
EOF
cat >"$bin/podman" <<'EOF'
#!/bin/sh
[ "$RUNTIME" = podman ] || exit 1
case "$1 $2" in
  'machine inspect') echo running ;;
  'machine ssh') echo "$VM_TIME" ;;
  *) exit 1 ;;
esac
EOF
cat >"$bin/docker" <<'EOF'
#!/bin/sh
case $1 in
  info) exit 0 ;;
  run) echo "$VM_TIME" ;;
  *) exit 1 ;;
esac
EOF
chmod +x "$bin/date" "$bin/podman" "$bin/docker"

# expect NAME RUNTIME HOST_TIME VM_TIME STATUS TEXT...: the clock check
# exits with STATUS and prints every TEXT.
expect() {
  name=$1
  shift
  RUNTIME=$1 HOST_TIME=$2 VM_TIME=$3 PATH="$bin:$PATH" sh "$scripts/container.sh" clock >"$work/out" 2>&1
  status=$?
  want=$4
  shift 4
  out=$(cat "$work/out")
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
    sed 's/^/      out: /' "$work/out"
  fi
}

expect 'podman in step' podman 1000 1000 0 'within 2s'
expect 'podman 2s behind' podman 1000 998 0 'within 2s'
expect 'podman 19 minutes behind' podman 2140 1000 1 '1140s behind' 'podman machine ssh sudo chronyc makestep'
expect 'podman 5s ahead' podman 1000 1005 1 '5s ahead' 'chronyc makestep'
expect 'docker in step' docker 1000 1001 0 'within 2s'
expect 'docker 1 minute behind' docker 1060 1000 1 '60s behind' 'restart Docker Desktop'

printf '%s passed, %s failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
