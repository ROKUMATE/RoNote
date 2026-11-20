#!/bin/sh
# Runs a command as the ronote user, with its own systemd and podman
# (docs/adr/0014-production-topology.md). Run it as root:
#
#   sudo sh deploy/as-ronote.sh systemctl --user status ronote-api
set -eu
uid=$(id -u ronote)
cd /
exec sudo -u ronote env XDG_RUNTIME_DIR="/run/user/$uid" \
	DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/$uid/bus" "$@"
