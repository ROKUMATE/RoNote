#!/bin/sh
# Deploys a release on this host (docs/adr/0015-operations.md), as root: the
# deploy user's forced SSH command, or by hand. bootstrap.sh installs it in
# /usr/local/lib/ronote beside as-ronote.sh, and /usr/local/bin/ronote-deploy
# runs it:
#
#   sudo ronote-deploy v1.2.3
#
#   1. the tag's deploy/ from the bare clone, into a directory of its own,
#      so the units and install.sh are the release's;
#   2. its three images, pulled as ronote and pinned by digest;
#   3. the migrations, run by the new api, and the schema checked;
#   4. the release's units with those digests (install.sh, whose failure
#      report says why a unit didn't start);
#   5. health: every unit healthy, and the api's /healthz naming the tag;
#   6. the digests recorded, the last ones kept as the previous release.
#
# On any failure it reinstalls the previous release's digests, which run on
# the migrated schema since migrations are expand/contract (ADR 0015), says
# so through an alert, and exits non-zero.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
state=${RONOTE_DEPLOY_STATE:-/var/lib/ronote/deploy}
clone=${RONOTE_DEPLOY_CLONE:-/opt/ronote/repo.git}
registry=${RONOTE_DEPLOY_REGISTRY:-ghcr.io/rokumate}
images="ronote-api ronote-worker ronote-web"

say() {
	printf 'ronote-deploy: %s\n' "$*"
}

tag=${1:-${SSH_ORIGINAL_COMMAND:-}}
if ! printf '%s' "$tag" | grep -Eqx 'v[0-9]+\.[0-9]+\.[0-9]+'; then
	say "want a release tag such as v1.2.3, got '$tag'" >&2
	exit 2
fi
[ "$(id -u)" -eq 0 ] || { say "run it as root" >&2; exit 2; }
mkdir -p "$state/releases"
mkdir "$state/lock" 2>/dev/null || { say "another deploy is running ($state/lock)" >&2; exit 1; }
trap 'rmdir "$state/lock"' EXIT

# install TAG PINS: the release's units with those pins.
install_release() {
	# shellcheck disable=SC2086 # two words a pin
	sh "$state/releases/$1/deploy/install.sh" --version "$1" $2
}

# as_user: its own copy, which works before a release is fetched.
as_user() {
	sh "$here/as-ronote.sh" "$@"
}

alert() {
	home=$(getent passwd ronote | cut -d: -f6)
	as_user sh "$home/.local/lib/ronote/alert.sh" "$1" "$2" || say "the alert wasn't sent: $1" >&2
}

# fail WHAT: goes back to the previous release, if any, and alerts.
fail() {
	say "$1" >&2
	if [ ! -f "$state/current" ]; then
		alert "deploying $tag failed" "$1; there is no earlier release to go back to"
		exit 1
	fi
	previous=$(sed -n 's/^tag=//p' "$state/current")
	if install_release "$previous" "$(sed -n 's/^pins=//p' "$state/current")"; then
		alert "deploying $tag failed" "$1; $previous runs again"
	else
		alert "deploying $tag failed, and so did going back" "$1; going back to $previous failed too: see journalctl"
	fi
	exit 1
}

say "deploying $tag"
release=$state/releases/$tag
rm -rf "$release"
mkdir -p "$release"
git --git-dir="$clone" fetch --quiet --force --tags origin || fail "can't fetch the tags"
git --git-dir="$clone" archive "$tag" deploy scripts/embed-check.sh | tar -x -C "$release" || fail "no tag $tag"

pins=
for image in $images; do
	ref=$registry/$image:$tag
	as_user podman pull -q "$ref" >/dev/null || fail "can't pull $ref"
	digest=$(as_user podman image inspect --format '{{.Digest}}' "$ref") || fail "can't read $ref's digest"
	pins="$pins --pin $image=$ref@$digest"
done
api=$(printf '%s' "$pins" | sed -n 's/.*--pin ronote-api=\([^ ]*\).*/\1/p')

migrate() {
	as_user podman run --rm --network ronote-data --secret ronote-database-url,type=env,target=RONOTE_DATABASE_URL "$api" migrate "$@"
}
migrate >/dev/null || fail "the migrations failed"
migrate status || fail "the schema isn't this release's"

install_release "$tag" "$pins" || fail "the units didn't start"
# The api's own word: Caddy, on its network, asks it for /healthz.
health=$(as_user podman exec ronote-caddy wget -qO- http://ronote-api:8080/healthz) || fail "the api doesn't answer /healthz"
case $health in
*"\"version\":\"$tag\""*) ;;
*) fail "the api runs another version: $health" ;;
esac

[ ! -f "$state/current" ] || mv "$state/current" "$state/previous"
printf 'tag=%s\npins=%s\n' "$tag" "${pins# }" >"$state/current"
say "$tag runs"
