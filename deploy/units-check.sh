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

# CI's drop-ins keep every container from pulling and cut its start timeout
# to five minutes. Quadlet before 5.2 ignores ronote-.container.d, which
# the dry run reports as no error, so what the services say is checked.
unchanged=$(awk '
	function flush() { if (run && (timeout != "TimeoutStartSec=300" || !pull)) print name }
	/^---.*---$/ { flush(); name = $0; sub(/^---/, "", name); sub(/---$/, "", name); timeout = ""; run = 0; pull = 0; next }
	/^TimeoutStartSec=/ { timeout = $0 }
	/^ExecStart=.*podman run/ { run = 1; if ($0 ~ / --pull never /) pull = 1 }
	END { flush() }
' "$dir/out")
if [ -n "$unchanged" ]; then
	printf 'units-check: CI drop-ins left these pulling or with production start timeout:\n%s\n(Quadlet %s; the drop-in ronote-.container.d needs 5.2)\n' \
		"$unchanged" "$("$quadlet" -version)" >&2
	exit 1
fi
printf 'units-check: Quadlet %s converts every unit\n' "$("$quadlet" -version)"
