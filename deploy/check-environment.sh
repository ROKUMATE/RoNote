#!/bin/sh
# Checks the environment of ronote's systemd, which `systemctl --user
# show-environment` prints on stdin (docs/adr/0014-production-topology.md):
# Quadlet's generator and every podman a unit starts read their directories
# from it, so another user's would put ronote's containers in that user's
# home. install.sh runs it; it prints each bad variable and exits 1.
#
#   systemctl --user show-environment | sh deploy/check-environment.sh UID HOME
set -eu

uid=${1:?the uid of the ronote user}
home=${2:?the home of the ronote user}
env=$(cat)

# value NAME: the variable's value, empty when it is not set.
value() {
	printf '%s\n' "$env" | sed -n "s/^$1=//p" | tail -n 1
}

bad=false
complain() {
	printf 'check-environment: %s is %s, but %s\n' "$1" "$2" "$3" >&2
	bad=true
}

runtime=$(value XDG_RUNTIME_DIR)
[ "$runtime" = "/run/user/$uid" ] || complain XDG_RUNTIME_DIR "${runtime:-unset}" "ronote's is /run/user/$uid"

for name in XDG_CONFIG_HOME XDG_DATA_HOME XDG_STATE_HOME XDG_CACHE_HOME; do
	dir=$(value "$name")
	case $dir in
	'' | "$home" | "$home"/*) ;;
	*) complain "$name" "$dir" "it must be unset or inside $home" ;;
	esac
done

if [ "$bad" = true ]; then
	cat >&2 <<EOF
check-environment: ronote's systemd got those from the host's environment:
  /etc/environment or /etc/security/pam_env.conf (pam_env hands them to every
  user manager), or a file in /etc/environment.d. They name another user's
  directories, as GitHub's runner image sets for its own user. Remove them
  there and restart ronote's manager: systemctl restart user@$uid.service.
EOF
	exit 1
fi
