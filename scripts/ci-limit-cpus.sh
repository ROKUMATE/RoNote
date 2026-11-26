#!/bin/sh
# Puts ronote's units on 2 CPUs on a CI runner, as on Oracle's Always Free
# host, whose 2 OCPUs are 2 cores in all (docs/adr/0014-production-topology.md):
# GitHub's arm64 runner has 4, on which the two Ollamas and docling never
# compete. The ronote user doesn't exist yet, so the limit goes on every
# user's slice, by systemd's prefix drop-in for user-*.slice, before
# bootstrap.sh makes the user: its slice is made with it, and every unit
# starts within it. make prod-memory reports the slice's CPUs. Run it on a
# CI runner, as the runner user with sudo:
#
#   sh scripts/ci-limit-cpus.sh [CPUS]     CPUS 0-1
set -eu

cpus=${1:-0-1}
dir=/etc/systemd/system/user-.slice.d
sudo mkdir -p "$dir"
printf '[Slice]\nAllowedCPUs=%s\n' "$cpus" | sudo tee "$dir/50-ronote-ci-cpus.conf" >/dev/null
sudo systemctl daemon-reload
echo "ci-limit-cpus: every user's slice runs on CPUs $cpus (of $(nproc --all))"
