#!/bin/sh
# Runs container tooling against whichever runtime is up: podman machine
# (macOS) or Docker (Linux).
#
#   container.sh compose ARGS...   docker/podman compose on deploy/compose.dev.yml
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

[ "$#" -gt 0 ] || die 'usage: container.sh compose ARGS...'
cmd=$1
shift
case $cmd in
  compose) compose "$@" ;;
  *) die "unknown command: $cmd" ;;
esac
