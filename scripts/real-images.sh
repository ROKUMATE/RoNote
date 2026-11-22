#!/bin/sh
# What the real docling and Ollama images need on this host
# (docs/adr/0015-operations.md, .github/workflows/arm-real.yml). Both
# subcommands write Markdown for the job summary to stdout, the reason for a
# failure to stderr, and exit 1 on a failure.
#
#   sh scripts/real-images.sh arm64     each pinned image is a multi-arch index
#                                       listing linux/amd64 and linux/arm64
#   sudo sh scripts/real-images.sh memory
#                                       each container's peak memory against
#                                       its limit, after a run; a container
#                                       the kernel killed for memory, or one
#                                       that restarted, fails it
set -u

here=$(cd "$(dirname "$0")" && pwd)
failed=false

say() {
	printf '%s\n' "$*"
}

fail() {
	failed=true
	printf 'real-images: %s\n' "$*" >&2
}

# as_user runs a command as ronote, with its own systemd and podman; a test
# stands in for it with RONOTE_AS_RONOTE.
as_user() {
	sh "${RONOTE_AS_RONOTE:-$here/../deploy/as-ronote.sh}" "$@"
}

# The images the units and the backups pin by digest.
pinned() {
	{
		sed -n 's/^Image=//p' "$here"/../deploy/quadlet/*.container "$here"/../deploy/quadlet/ci/*.container
		sed -n 's/^[a-z]*=//p' "$here/../deploy/backup/images"
	} | grep '@sha256:' | sort -u
}

# by_digest REF: REF without its tag. podman refuses a reference with both a
# tag and a digest ("currently not supported"), and the digest is what a unit
# runs by.
by_digest() {
	name=${1%%@*}
	case ${name##*/} in
	*:*) name=${name%:*} ;;
	esac
	printf '%s@%s' "$name" "${1#*@}"
}

# arm64: each pin is the multi-arch index, listing linux/amd64 and
# linux/arm64. A pin that is one architecture's manifest would run that
# image on the other, and one podman can't read is no verdict on its builds,
# so each says what it is.
arm64() {
	say "### Pinned images: multi-arch indexes with linux/arm64"
	say ""
	say "| image | index | linux/amd64 | linux/arm64 |"
	say "|---|---|---|---|"
	errors=$(mktemp)
	trap 'rm -f "$errors"' EXIT
	broken=false
	refs=$(pinned)
	for ref in $refs; do
		if ! manifest=$(podman manifest inspect "$(by_digest "$ref")" 2>"$errors"); then
			say "| \`$ref\` | **error** | - | - |"
			fail "can't inspect $ref: $(cat "$errors")"
			continue
		fi
		flat=$(printf '%s' "$manifest" | tr -d ' \t\r\n')
		case $flat in
		*'"manifests":['*) ;;
		*)
			say "| \`$ref\` | **single** | - | - |"
			fail "$ref is a single-arch manifest, not a multi-arch index: production on arm64 would pull the wrong image"
			broken=true
			continue
			;;
		esac
		platforms=$(printf '%s' "$flat" | grep -o '"platform":{[^}]*}' | grep '"os":"linux"')
		row="| \`$ref\` | yes |"
		for arch in amd64 arm64; do
			if printf '%s\n' "$platforms" | grep -q "\"architecture\":\"$arch\""; then
				row="$row yes |"
			else
				row="$row **no** |"
				fail "no linux/$arch entry in the index of $ref"
				broken=true
			fi
		done
		say "$row"
	done
	if [ "$broken" = true ]; then
		say ""
		say "A pin that is not an index listing both platforms can't run on both the amd64 hosts ADR 0015 allows and Oracle's arm64 A1. Options: pin the image's index digest (the one of its tag); pick a release that has both builds; or build it for the missing architecture and pin that."
	fi
}

mib() {
	echo $(($1 / 1048576))
}

memory() {
	say "### Peak memory of each container"
	say ""
	say "| container | peak | limit | of limit | state |"
	say "|---|---|---|---|---|"
	units=$(as_user systemctl --user list-units --all --plain --no-legend 'ronote-*.service' | awk '{ print $1 }')
	for unit in $units; do
		name=${unit%.service}
		as_user podman container exists "$name" 2>/dev/null || continue
		peak=$(as_user systemctl --user show -p MemoryPeak --value "$unit")
		limit=$(as_user podman inspect --format '{{.HostConfig.Memory}}' "$name")
		killed=$(as_user podman inspect --format '{{.State.OOMKilled}}' "$name")
		restarts=$(as_user systemctl --user show -p NRestarts --value "$unit")
		state=ok
		case $peak in
		'' | *[!0-9]*) say "| $name | n/a | $(mib "${limit:-0}") MiB | | $state |"; continue ;;
		esac
		percent=0
		[ "${limit:-0}" -gt 0 ] && percent=$((peak * 100 / limit))
		if [ "$killed" = true ]; then
			state="**OOM-killed**"
			fail "$name was killed for memory at its $(mib "$limit") MiB limit"
		elif [ "${restarts:-0}" -gt 0 ]; then
			state="**restarted $restarts times**"
			fail "$name restarted $restarts times: it did not stay within its $(mib "$limit") MiB limit, or failed some other way (see its journal)"
		elif [ "$percent" -ge 90 ]; then
			state="near its limit"
		fi
		say "| $name | $(mib "$peak") MiB | $(mib "${limit:-0}") MiB | $percent% | $state |"
	done
	if [ "$failed" = true ]; then
		say ""
		say "Docling is the unit to doubt: it is given 4 GiB of the 12 GB host (ADR 0014), the most of any. Options, none applied here: raise its limit to 5-6 GiB, paid for from the host's 2.3 GB of headroom; cap what it converts at once (\`DOCLING_SERVE_MAX_NUM_PAGES\`, fewer workers); or give production the whole Always Free allowance, 4 OCPUs and 24 GB, and raise the limits to match."
	fi
}

case ${1:-} in
arm64) arm64 ;;
memory) memory ;;
*)
	printf 'usage: sh scripts/real-images.sh arm64|memory\n' >&2
	exit 2
	;;
esac
[ "$failed" = false ]
