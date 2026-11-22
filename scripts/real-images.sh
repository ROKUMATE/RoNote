#!/bin/sh
# What the real docling and Ollama images need on this host
# (docs/adr/0015-operations.md, .github/workflows/arm-real.yml). Both
# subcommands write Markdown for the job summary to stdout, the reason for a
# failure to stderr, and exit 1 on a failure.
#
#   sh scripts/real-images.sh arm64     each real image has a linux/arm64 build
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

arm64() {
	say "### Real images on arm64"
	say ""
	say "| image | linux/arm64 |"
	say "|---|---|"
	# pdftext runs docling's image: two images cover the three real units.
	refs=$(sed -n 's/^Image=//p' "$here/../deploy/quadlet/ronote-ollama.container" "$here/../deploy/quadlet/ronote-docling.container" | sort -u)
	for ref in $refs; do
		if manifest=$(podman manifest inspect "$ref" 2>&1) && printf '%s\n' "$manifest" | grep -q '"architecture": *"arm64"'; then
			say "| \`$ref\` | yes |"
		else
			say "| \`$ref\` | **no** |"
			fail "no linux/arm64 build of $ref"
		fi
	done
	if [ "$failed" = true ]; then
		say ""
		say "An image without an arm64 build can't run on the production host (Oracle's A1 is arm64). Options: pick a release of it that has one; build it for arm64 and pin that; or run production on an amd64 host, which ADR 0015 allows."
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
