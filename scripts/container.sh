#!/bin/sh
# Runs container tooling against whichever runtime is up: podman machine
# (macOS) or Docker (Linux).
#
#   container.sh compose ARGS...   docker/podman compose on deploy/compose.dev.yml
#   container.sh test CMD...       run CMD with testcontainers pointed at the
#                                  runtime, then fail if it leaked containers
#   container.sh clean             remove leftover testcontainers containers
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

[ "$#" -gt 0 ] || die 'usage: container.sh compose|test|clean [ARGS...]'
cmd=$1
shift
case $cmd in
  compose) compose "$@" ;;
  test) run_tests "$@" ;;
  clean) clean ;;
  *) die "unknown command: $cmd" ;;
esac
