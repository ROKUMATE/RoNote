#!/bin/sh
# What backup.sh and restore-drill.sh share (docs/adr/0015-operations.md):
# the pinned images, and restic on the repository. Sourced, after setting
# name, the word their messages start with.

etc=${RONOTE_ETC:-$HOME/.config/ronote}

say() {
	printf '%s: %s\n' "${name:?set name before sourcing lib.sh}" "$*"
}

# image NAME: the pinned image backup.images names.
image() {
	ref=$(sed -n "s/^$1=//p" "$etc/backup.images")
	[ -n "$ref" ] || { say "no $1 image in $etc/backup.images" >&2; exit 1; }
	printf '%s' "$ref"
}

# restic [PODMAN-OPTION...] -- ARG...: restic on the repository, read-only
# but for its cache, with nothing to reach but the repository.
restic() {
	opts=
	while [ "$1" != -- ]; do
		opts="$opts $1"
		shift
	done
	shift
	# shellcheck disable=SC2086 # one word per option
	# --log-driver=none: what a container says reaches the journal only as
	# the script passes it on.
	podman run --rm --log-driver=none --read-only --cap-drop=all --security-opt=no-new-privileges --memory=512m \
		--network ronote-egress \
		--secret ronote-restic-password,type=env,target=RESTIC_PASSWORD \
		--secret ronote-backup-repo,type=env,target=RESTIC_REPOSITORY \
		--secret ronote-backup-access-key,type=env,target=AWS_ACCESS_KEY_ID \
		--secret ronote-backup-secret-key,type=env,target=AWS_SECRET_ACCESS_KEY \
		-e RESTIC_CACHE_DIR=/var/lib/restic/cache -v ronote-backup:/var/lib/restic:Z --tmpfs /tmp $opts \
		"$(image restic)" "$@"
}
