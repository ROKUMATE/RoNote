#!/bin/sh
# Lets podman signal pasta under Ubuntu's AppArmor profile for pasta
# (docs/adr/0015-operations.md). bootstrap.sh runs it as root:
#
#   sh deploy/pasta-apparmor.sh [/etc/apparmor.d]
#
# Ubuntu confines podman under a profile of its own name, and passt's profile
# for pasta takes no signal from it, so podman can't stop pasta when the last
# container leaves the rootless network: "rootless netns: kill network
# process: permission denied", on every stop, restart and deploy. The fix is
# upstream's patch, not yet merged or shipped, "apparmor: Fix signaling issues
# from podman" (https://bugs.launchpad.net/ubuntu/+source/passt/+bug/2154379):
# its exact lines, after the profile's include of pasta's abstraction, as the
# patch puts them. The profile includes no local/ file, so they go into the
# profile itself. A profile, or pasta's abstractions, that lets podman signal
# pasta already is left alone, so once Ubuntu ships the fix this does nothing.
# It prints what it did, and reloads that one profile.
set -eu

dir=${1:-/etc/apparmor.d}
profile=$dir/usr.bin.pasta

say() {
	printf 'pasta-apparmor: %s\n' "$*"
}

die() {
	say "$*" >&2
	exit 1
}

if [ ! -f "$profile" ]; then
	say "no $profile: pasta isn't confined here; nothing to do"
	exit 0
fi

# allows_podman FILE...: whether one of them has a signal rule letting pasta
# receive from podman: no access list or one that receives, and no peer or
# podman. A comment, or a deny rule, is none.
allows_podman() {
	awk '
		{ sub(/^[ \t]+/, "") }
		/^#/ { next }
		{ sub(/[ \t]+#.*/, "") }
		/^(audit[ \t]+)?(allow[ \t]+)?signal([ \t(,]|$)/ {
			rule = $0
			sub(/^(audit[ \t]+)?(allow[ \t]+)?signal/, "", rule)
			peer = ""
			if (match(rule, /peer=[^ \t,]+/)) peer = substr(rule, RSTART + 5, RLENGTH - 5)
			gsub(/"/, "", peer)
			if (peer != "" && peer != "podman") next
			access = rule
			sub(/(set|peer)=.*/, "", access)
			gsub(/[(),]/, " ", access)
			n = split(access, words, /[ \t]+/)
			named = 0
			receives = 0
			for (i = 1; i <= n; i++) {
				if (words[i] == "") continue
				named = 1
				if (words[i] ~ /^(receive|r|read|rw)$/) receives = 1
			}
			if (!named || receives) found = 1
		}
		END { exit !found }
	' "$@"
}

abstractions=
for f in "$dir/abstractions/pasta" "$dir/abstractions/passt"; do
	[ ! -f "$f" ] || abstractions="$abstractions $f"
done
# shellcheck disable=SC2086 # one word per file
if allows_podman "$profile" $abstractions; then
	say "$profile already lets podman signal pasta"
	exit 0
fi
grep -q '^[[:space:]]*include <abstractions/pasta>[[:space:]]*$' "$profile" ||
	die "$profile has no include of <abstractions/pasta>, the line upstream's patch goes after; left alone"

new=$(mktemp)
kept=$(mktemp)
trap 'rm -f "$new" "$kept"' EXIT
awk '
	{ print }
	!done && /^[ \t]*include <abstractions\/pasta>[ \t]*$/ {
		print ""
		print "  # Allow podman to signal pasta"
		print "  signal (receive) peer=podman,"
		done = 1
	}
' "$profile" >"$new"
cp "$profile" "$kept"
# Written in place, keeping the file's owner and mode: a copy left beside it
# in the directory would be loaded at boot as a second profile.
cat "$new" >"$profile"
say "$profile now lets podman signal pasta (upstream's lines)"

if ! command -v aa-enabled >/dev/null 2>&1 || ! aa-enabled --quiet 2>/dev/null; then
	say "AppArmor is off: nothing loaded; the rule takes effect when it is on"
	exit 0
fi
if ! apparmor_parser -r "$profile"; then
	cat "$kept" >"$profile"
	die "apparmor_parser refused $profile with the rule; it is back as it was"
fi
say "reloaded $profile"
