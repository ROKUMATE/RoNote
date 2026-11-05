#!/bin/sh
# Runs container tooling against whichever runtime is up: podman machine
# (macOS) or Docker (Linux).
#
#   container.sh compose ARGS...   docker/podman compose on deploy/compose.dev.yml
#   container.sh test CMD...       run CMD with testcontainers pointed at the
#                                  runtime, then fail if it leaked containers
#   container.sh clean             remove leftover testcontainers containers
#   container.sh clock             fail unless the container VM's clock is
#                                  within 2 seconds of the host's
set -eu

compose_file=$(cd "$(dirname "$0")/.." && pwd)/deploy/compose.dev.yml

die() {
  printf 'container.sh: %s\n' "$*" >&2
  exit 1
}

compose() {
  if podman compose version >/dev/null 2>&1; then
    PODMAN_COMPOSE_WARNING_LOGS=false exec podman compose -f "$compose_file" "$@"
  fi
  if docker compose version >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
    exec docker compose -f "$compose_file" "$@"
  fi
  die 'neither "podman compose" nor "docker compose" works.
  Podman: run "podman machine start" and install a compose provider (brew install docker-compose).
  Docker: start the Docker daemon and install the compose plugin.'
}

# use_runtime sets rt to podman when the podman machine is running, or to
# docker when the Docker daemon answers.
use_runtime() {
  if [ "$(podman machine inspect --format '{{.State}}' 2>/dev/null)" = running ]; then
    rt=podman
  elif docker info >/dev/null 2>&1; then
    rt=docker
  else
    die 'no container runtime is reachable. Run "podman machine start" (macOS) or start Docker (Linux).'
  fi
}

run_tests() {
  use_runtime
  if [ "$rt" = podman ]; then
    : "${DOCKER_HOST:=unix://$(podman machine inspect --format '{{.ConnectionInfo.PodmanSocket.Path}}')}"
    : "${TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE:=/var/run/docker.sock}"
    : "${TESTCONTAINERS_RYUK_DISABLED:=true}"
    export DOCKER_HOST TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE TESTCONTAINERS_RYUK_DISABLED
  fi

  status=0
  "$@" || status=$?

  leaked=$("$rt" ps -aq --filter label=ronote.pgtest=true)
  if [ -n "$leaked" ]; then
    die "tests left these containers behind; run \"make test-clean\":
$leaked"
  fi
  exit "$status"
}

clean() {
  use_runtime
  ids=$("$rt" ps -aq --filter label=org.testcontainers=true)
  if [ -z "$ids" ]; then
    echo 'container.sh: no testcontainers containers to remove'
    return
  fi
  # shellcheck disable=SC2086 # ids is a list of container IDs, one per word
  "$rt" rm -f -v $ids
}

# clock compares the container VM's clock with the host's. Postgres runs on
# the VM's clock and the app on the host's; podman machine's clock can lag
# for minutes after the host sleeps. Each clock is read to the second, the
# VM's between two readings of the host's.
clock() {
  use_runtime
  before=$(date +%s)
  if [ "$rt" = podman ]; then
    vm=$(podman machine ssh date +%s) || die 'could not read the podman machine clock'
    fix='podman machine ssh sudo chronyc makestep'
  else
    image=$(sed -n 's/^ *image: *//p' "$compose_file" | head -n 1)
    vm=$(docker run --rm --pull=never --entrypoint date "$image" +%s) ||
      die "could not run $image; run \"make dev-up\" first to pull it"
    fix='restart Docker Desktop'
  fi
  after=$(date +%s)
  if [ "$vm" -lt $((before - 2)) ]; then
    off=$((before - vm))
    side=behind
  elif [ "$vm" -gt $((after + 2)) ]; then
    off=$((vm - after))
    side=ahead
  else
    echo "doctor: the $rt VM's clock is within 2s of the host's"
    return
  fi
  die "the $rt VM's clock is ${off}s $side the host's, so the database's times are off from the app's.
  Fix: $fix"
}

[ "$#" -gt 0 ] || die 'usage: container.sh compose|test|clean|clock [ARGS...]'
cmd=$1
shift
case $cmd in
  compose) compose "$@" ;;
  test) run_tests "$@" ;;
  clean) clean ;;
  clock) clock ;;
  *) die "unknown command: $cmd" ;;
esac
