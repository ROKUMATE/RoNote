#!/bin/sh
# make deploy-host TAG=v1.2.3, from the release's deploy job
# (docs/adr/0015-operations.md): asks the host to deploy TAG over SSH, as the
# deploy user, whose key runs ronote-deploy and nothing else. The key and the
# host's known key come from the environment, are written where only this
# user can read them, and are never printed.
#
#   DEPLOY_HOST, DEPLOY_SSH_KEY (a private key), DEPLOY_KNOWN_HOSTS (a line)
set -eu

die() {
	printf 'deploy-host: %s\n' "$*" >&2
	exit 1
}

tag=${1:-}
printf '%s' "$tag" | grep -Eqx 'v[0-9]+\.[0-9]+\.[0-9]+' || die "want a release tag such as v1.2.3, got '$tag'"
[ -n "${DEPLOY_HOST:-}" ] || die "set DEPLOY_HOST"
[ -n "${DEPLOY_SSH_KEY:-}" ] || die "set DEPLOY_SSH_KEY"
[ -n "${DEPLOY_KNOWN_HOSTS:-}" ] || die "set DEPLOY_KNOWN_HOSTS: the host's key, so nothing else can pose as it"

dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
umask 077
printf '%s\n' "$DEPLOY_SSH_KEY" >"$dir/key"
printf '%s\n' "$DEPLOY_KNOWN_HOSTS" >"$dir/known_hosts"
ssh -i "$dir/key" -o IdentitiesOnly=yes -o UserKnownHostsFile="$dir/known_hosts" -o StrictHostKeyChecking=yes \
	-o BatchMode=yes "deploy@$DEPLOY_HOST" "$tag"
