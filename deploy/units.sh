#!/bin/sh
# Writes the Quadlet unit set into DIR, as install.sh installs it and
# units-check.sh converts it (docs/adr/0014-production-topology.md): the
# units with their images' tag, without SeaweedFS when the blobs live in an
# external S3, and the drop-ins added beside them.
#
#   sh deploy/units.sh DIR VERSION [--no-seaweedfs] [--drop-ins DIR]
set -eu

here=$(cd "$(dirname "$0")" && pwd)
dir=${1:?the directory to write the units into}
version=${2:?the images tag, such as v1.2.3}
shift 2
seaweedfs=true
dropins=
while [ $# -gt 0 ]; do
	case $1 in
	--no-seaweedfs) seaweedfs=false; shift ;;
	--drop-ins) dropins=${2:?}; shift 2 ;;
	*) printf 'units: unknown argument %s\n' "$1" >&2; exit 1 ;;
	esac
done

for f in "$here"/quadlet/ronote-*; do
	case $f in */ronote-seaweedfs.container | */ronote-blobs.volume) [ "$seaweedfs" = true ] || continue ;; esac
	sed "s|@RONOTE_VERSION@|$version|g" "$f" >"$dir/$(basename "$f")"
done
if [ -n "$dropins" ]; then
	for f in "$dropins"/ronote-*; do
		cp -R "$f" "$dir"/
	done
fi
