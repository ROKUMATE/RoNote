#!/bin/sh
# make units-check: converts the unit sets install.sh installs in CI, with
# Quadlet's dry run, so a unit this Podman rejects fails here with Quadlet's
# own message: the fakes' set (deploy/quadlet/ci and ci-fakes) and the real
# images' (ci and real, docs/adr/0015-operations.md). Where Quadlet isn't
# installed (macOS), it says so and passes. QUADLET names another Quadlet
# binary.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
quadlet=${QUADLET:-/usr/libexec/podman/quadlet}
if [ ! -x "$quadlet" ]; then
	printf 'units-check: skipped, no Quadlet at %s\n' "$quadlet"
	exit 0
fi

dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT

# check NAME REAL LAYER...: converts the units with those layers of drop-ins.
# Every container is to pull nothing (Pull=never) and start within five
# minutes, but the services named in REAL (the real images' own, which the
# start pulls) to pull what is missing and have 25 minutes. Quadlet before
# 5.2 ignores ronote-.container.d, which the dry run reports as no error, and
# drop-ins apply by file name, whichever directory they are in, so what the
# services say is checked.
check() {
	name=$1
	real=$2
	shift 2
	set=$dir/$name
	mkdir -p "$set/units"
	layers=
	for layer in "$@"; do
		layers="$layers --drop-ins $here/quadlet/$layer"
	done
	# shellcheck disable=SC2086 # one word per layer
	sh "$here/units.sh" "$set/units" check $layers

	# Quadlet logs each unit it loads; only its errors are shown.
	status=0
	QUADLET_UNIT_DIRS=$set/units "$quadlet" -dryrun -user >"$set/out" 2>"$set/err" || status=$?
	if [ "$status" -ne 0 ]; then
		grep -v 'Loading source' "$set/err" >&2 || true
		printf 'units-check: Quadlet %s rejected the %s units (exit %s)\n' "$("$quadlet" -version)" "$name" "$status" >&2
		exit 1
	fi
	wrong=$(awk -v real="$real" '
		function flush(   own) {
			if (!run) return
			own = index(" " real " ", " " name " ") > 0
			if (timeout != (own ? "TimeoutStartSec=1500" : "TimeoutStartSec=300") || pull != (own ? "missing" : "never")) print name
		}
		/^---.*---$/ { flush(); name = $0; sub(/^---/, "", name); sub(/---$/, "", name); timeout = ""; run = 0; pull = ""; next }
		/^TimeoutStartSec=/ { timeout = $0 }
		/^ExecStart=.*podman run/ { run = 1; if (match($0, / --pull [a-z]+ /)) pull = substr($0, RSTART + 8, RLENGTH - 9) }
		END { flush() }
	' "$set/out")
	if [ -n "$wrong" ]; then
		printf 'units-check: with the %s drop-ins these have the wrong pull policy or start timeout:\n%s\n(Quadlet %s: drop-ins apply by file name, and ronote-.container.d needs 5.2)\n' \
			"$name" "$wrong" "$("$quadlet" -version)" >&2
		exit 1
	fi
}

check fakes '' ci ci-fakes
check real 'ronote-ollama.service ronote-ollama-query.service ronote-docling.service ronote-pdftext.service' ci real
printf 'units-check: Quadlet %s converts every unit, with the fakes and with the real images\n' "$("$quadlet" -version)"
