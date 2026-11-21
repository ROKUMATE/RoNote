#!/bin/sh
# make units-check: converts the unit set install.sh installs, CI's drop-ins
# included, with Quadlet's dry run, so a unit this Podman rejects fails here
# with Quadlet's own message. Where Quadlet isn't installed (macOS), it says
# so and passes. QUADLET names another Quadlet binary.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
quadlet=${QUADLET:-/usr/libexec/podman/quadlet}
if [ ! -x "$quadlet" ]; then
	printf 'units-check: skipped, no Quadlet at %s\n' "$quadlet"
	exit 0
fi

dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
mkdir "$dir/units"
sh "$here/units.sh" "$dir/units" check --drop-ins "$here/quadlet/ci"

# Quadlet logs each unit it loads; only its errors are shown.
status=0
QUADLET_UNIT_DIRS=$dir/units "$quadlet" -dryrun -user >"$dir/out" 2>"$dir/err" || status=$?
if [ "$status" -ne 0 ]; then
	grep -v 'Loading source' "$dir/err" >&2 || true
	printf 'units-check: Quadlet %s rejected the units (exit %s)\n' "$("$quadlet" -version)" "$status" >&2
	exit 1
fi
printf 'units-check: Quadlet %s converts every unit\n' "$("$quadlet" -version)"
