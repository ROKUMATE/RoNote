#!/bin/sh
# Gives a host without swap a 2 GiB swap file, with vm.swappiness 10
# (docs/adr/0014-production-topology.md). bootstrap.sh runs it as root:
#
#   sh deploy/host-swap.sh [ROOT]
#
# The units' limits fit the host's memory without it; it is for when their
# peaks coincide, so the kernel pages out something idle rather than kill a
# container. Swappiness 10 keeps it for that. A host with swap of its own
# is left alone; ours is made once, and made again if it is off. ROOT is
# where /proc, /etc and the file are (the tests' made-up host).
set -eu

root=${1:-}
file=/swapfile
conf=$root/etc/sysctl.d/90-ronote-swap.conf

say() {
	printf 'host-swap: %s\n' "$*"
}

active=$(awk 'NR > 1 { print $1 }' "$root/proc/swaps")
if [ -n "$active" ] && ! printf '%s\n' "$active" | grep -qx "$file"; then
	say "the host has swap of its own ($(printf '%s\n' "$active" | paste -sd ' ' -)): left alone"
	exit 0
fi

changed=false
if [ -z "$active" ]; then
	rm -f "$root$file"
	fallocate -l 2G "$root$file" 2>/dev/null || dd if=/dev/zero of="$root$file" bs=1M count=2048 2>/dev/null
	chmod 600 "$root$file"
	mkswap "$root$file" >/dev/null
	swapon "$root$file"
	changed=true
fi
if ! grep -q "^${file}[[:space:]]" "$root/etc/fstab"; then
	printf '%s none swap sw 0 0\n' "$file" >>"$root/etc/fstab"
	changed=true
fi
if [ "$(cat "$conf" 2>/dev/null)" != 'vm.swappiness = 10' ]; then
	printf 'vm.swappiness = 10\n' >"$conf"
	sysctl -q -p "$conf"
	changed=true
fi
if [ "$changed" = true ]; then
	say "$root$file: 2 GiB of swap, on now and at boot, with swappiness 10"
else
	say "$root$file is on, with swappiness 10"
fi
