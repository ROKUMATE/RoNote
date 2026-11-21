#!/bin/sh
# Says why units didn't start (docs/adr/0014-production-topology.md): for
# each service that isn't active, its status, its journal, its container's
# log, the container's health record, and one health check run by hand.
# install.sh runs it when `systemctl --user restart` fails, since systemd
# itself says only that a unit timed out. Nothing printed holds a secret:
# units name their secrets, never their values, and podman inspect is cut
# to the health record.
#
#   sh deploy/report-failed.sh RUNNER SERVICE...
#
# RUNNER is a script that runs its arguments as the ronote user
# (deploy/as-ronote.sh); it is run as `sh RUNNER COMMAND...`.
set -u

runner=${1:?the script that runs a command as ronote}
shift

as_user() {
	sh "$runner" "$@"
}

for service in "$@"; do
	as_user systemctl --user is-active --quiet "$service" && continue
	# Every unit's container is named as its service is.
	container=${service%.service}
	printf '\n==== %s is not active ====\n' "$service"
	printf '\n-- systemctl --user status\n'
	as_user systemctl --user status --no-pager -l "$service"
	printf '\n-- journalctl --user -u %s -n 200\n' "$service"
	as_user journalctl --user -u "$service" -n 200 --no-pager
	printf '\n-- podman logs --tail 200 %s\n' "$container"
	as_user podman logs --tail 200 "$container" 2>&1
	printf '\n-- podman inspect: the health record\n'
	as_user podman inspect --format '{{json .State.Health}}' "$container" 2>&1
	printf '\n-- podman healthcheck run %s\n' "$container"
	if output=$(as_user podman healthcheck run "$container" 2>&1); then
		printf 'healthy: %s\n' "$output"
	else
		printf 'failed, exit %s: %s\n' "$?" "$output"
	fi
done
exit 0
