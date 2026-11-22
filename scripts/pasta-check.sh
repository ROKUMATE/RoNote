#!/bin/sh
# Whether podman may stop pasta under AppArmor on this Ubuntu 26.04 host
# (deploy/pasta-apparmor.sh, docs/adr/0015-operations.md), for CI.
#
#   sh scripts/pasta-check.sh evidence   before bootstrap.sh: passt's version,
#                                        the signal rules pasta's profile has,
#                                        the reproducer as this user and the
#                                        kernel's AppArmor denials; never fails
#   sudo sh scripts/pasta-check.sh check after the install: every unit
#                                        stopped, the reproducer as ronote with
#                                        no container holding the network, every
#                                        unit started, and no AppArmor denial
#                                        nor "kill network process" meanwhile
set -u

here=$(cd "$(dirname "$0")" && pwd)
reproducer='podman unshare --rootless-netns true'

say() {
	printf 'pasta-check: %s\n' "$*"
}

die() {
	say "$*" >&2
	exit 1
}

as_user() {
	sh "${RONOTE_AS_RONOTE:-$here/../deploy/as-ronote.sh}" "$@"
}

evidence() {
	say "passt $(dpkg-query -W -f '${Version}' passt 2>/dev/null || echo 'not installed'), $(podman --version)"
	say "signal rules in pasta's profile and abstractions:"
	grep -Hn 'signal' /etc/apparmor.d/usr.bin.pasta /etc/apparmor.d/abstractions/pasta /etc/apparmor.d/abstractions/passt 2>&1 || say "(none)"
	say "the loaded profiles named pasta or podman:"
	sudo aa-status 2>&1 | grep -E '^ +(pasta|podman)$' || say "(none)"
	say "$reproducer, as $(id -un):"
	# shellcheck disable=SC2086 # the reproducer's words
	out=$($reproducer 2>&1)
	say "exit $?${out:+: $out}"
	say "the kernel's AppArmor denials since boot:"
	sudo journalctl -k -b --no-pager -o short-precise | grep 'apparmor="DENIED"' || say "(none)"
}

check() {
	since=$(date +%s)
	units=$(as_user systemctl --user list-units --plain --no-legend --state=active 'ronote-*.service' |
		awk '$1 !~ /-(network|volume)\.service$/ { print $1 }')
	[ -n "$units" ] || die "no ronote unit is active to stop and start"
	# shellcheck disable=SC2086 # one word per unit
	as_user systemctl --user stop $units || die "stopping the units failed"
	# With no container left on it, the rootless network is made for the
	# reproducer and torn down after: podman stops pasta.
	# shellcheck disable=SC2086 # the reproducer's words
	out=$(as_user $reproducer 2>&1) || die "$reproducer, as ronote, failed: $out"
	say "$reproducer, as ronote, every unit stopped: exit 0"
	# shellcheck disable=SC2086 # one word per unit
	if ! as_user systemctl --user start $units; then
		# shellcheck disable=SC2086 # one word per unit
		sh "$here/../deploy/report-failed.sh" "${RONOTE_AS_RONOTE:-$here/../deploy/as-ronote.sh}" $units
		die "a unit didn't start again; why is above"
	fi
	say "stopped and started: $(printf '%s\n' "$units" | paste -sd ' ' -)"

	failed=false
	if found=$(journalctl -k --no-pager -o short-precise --since "@$since" | grep 'apparmor="DENIED"'); then
		say "AppArmor denied, since the check began:"
		printf '%s\n' "$found"
		failed=true
	fi
	if found=$(as_user journalctl --user --no-pager --since "@$since" -u 'ronote-*' | grep 'kill network process'); then
		say "podman couldn't stop pasta:"
		printf '%s\n' "$found"
		failed=true
	fi
	[ "$failed" = false ] || die "podman may not stop pasta; see above (deploy/pasta-apparmor.sh)"
	say "no AppArmor denial, and podman stopped pasta"
}

case ${1:-} in
evidence) evidence ;;
check) check ;;
*)
	printf 'usage: sh scripts/pasta-check.sh evidence|check\n' >&2
	exit 2
	;;
esac
