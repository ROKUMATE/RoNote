#!/bin/sh
# GitHub's runner image writes its own user's XDG_* directories into
# /etc/environment, and pam_env hands them to ronote's systemd, whose Quadlet
# generator then opened /home/runner's and found no units
# (docs/adr/0014-production-topology.md). install.sh refuses that, so they go
# before it runs. Names only are printed, but for the XDG_* lines, which are
# directories. Run it on a CI runner, as the runner user with sudo.
set -eu

echo "/etc/environment sets:"
sed -n 's/^\([A-Za-z_][A-Za-z0-9_]*\)=.*/  \1/p' /etc/environment
sudo cp /etc/environment /etc/environment.runner
echo "removing from /etc/environment (a copy is /etc/environment.runner):"
grep '^XDG_' /etc/environment || true
sudo sed -i '/^XDG_/d' /etc/environment
if id ronote >/dev/null 2>&1; then sudo systemctl restart "user@$(id -u ronote).service"; fi
