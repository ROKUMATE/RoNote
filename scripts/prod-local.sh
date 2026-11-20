#!/bin/sh
# make prod-local (docs/adr/0014-production-topology.md): installs the
# production units with the fakes in the podman machine's VM, as CI does on
# its runner, then runs the smoke suite from this Mac through an ssh tunnel
# from 127.0.0.1:8443 to the VM's 443: macOS lets a user bind 443 only on
# every address, which would reach Caddy from the network. Chromium maps
# *.test to the tunnel (playwright.smoke.config.ts). make prod-config has built
# bin/prod-config and bin/ronote-images.tar.
set -eu

die() {
	printf 'prod-local: %s\n' "$*" >&2
	exit 1
}

# shellcheck disable=SC2016 # Go templates, not shell
ssh_config=$(podman machine inspect --format '{{.SSHConfig.IdentityPath}} {{.SSHConfig.Port}} {{.SSHConfig.RemoteUsername}}') ||
	die "no podman machine: podman machine start"
# shellcheck disable=SC2086 # three words: identity, port, login
set -- $ssh_config
key=$1
port=$2
login=$3
remote() {
	ssh -i "$key" -p "$port" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR "$login@localhost" "$@"
}

dir=/var/tmp/ronote
printf 'prod-local: copying the units, configuration and images into the VM\n'
remote "sudo rm -rf $dir && sudo mkdir -p $dir && sudo chmod 755 $dir"
tar -cf - deploy scripts/embed-check.sh bin/prod-config bin/ronote-images.tar | remote "sudo tar -xf - -C $dir"

remote "sudo env RONOTE_GITHUB_CLIENT_SECRET=ronote-e2e-secret sh $dir/deploy/install.sh --version local --config $dir/bin/prod-config --drop-ins $dir/deploy/quadlet/ci --images $dir/bin/ronote-images.tar --skip-model"
remote "sudo sh $dir/deploy/as-ronote.sh podman exec ronote-caddy cat /caddy/data/caddy/pki/authorities/local/intermediate.crt" >bin/smoke-ca.pem

# The dev stack's own containers publish its ports in the same VM, so only
# ronote's listening ports are checked here; CI also checks they refuse.
remote "sh $dir/deploy/check-ports.sh"

socket=bin/prod-local.ssh
rm -f "$socket"
ssh -i "$key" -p "$port" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR \
	-o ExitOnForwardFailure=yes -M -S "$socket" -f -N -L 127.0.0.1:8443:127.0.0.1:443 "$login@localhost" ||
	die "can't forward 127.0.0.1:8443 to the VM: is something else on it?"
status=0
RONOTE_SMOKE_HOST=127.0.0.1:8443 make prod-smoke || status=$?
ssh -S "$socket" -O exit "$login@localhost" 2>/dev/null || true
exit "$status"
