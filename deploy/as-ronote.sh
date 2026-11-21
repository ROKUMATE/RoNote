#!/bin/sh
# Runs a command as the ronote user, with its own systemd and podman
# (docs/adr/0014-production-topology.md). Run it as root:
#
#   sudo sh deploy/as-ronote.sh systemctl --user status ronote-api
#
# sudo may keep the caller's HOME and XDG dirs (GitHub's runner does), and
# podman would then read the caller's config, so ronote gets its own.
set -eu
uid=$(id -u ronote)
home=$(getent passwd ronote | cut -d: -f6)
cd /
exec sudo -u ronote env -u XDG_CONFIG_HOME -u XDG_DATA_HOME -u XDG_CACHE_HOME -u XDG_STATE_HOME \
	HOME="$home" XDG_RUNTIME_DIR="/run/user/$uid" \
	DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/$uid/bus" "$@"
