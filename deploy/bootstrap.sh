#!/bin/sh
# Prepares a fresh Ubuntu 26.04 host for RoNote (docs/adr/0015-operations.md),
# then installs it; running it again changes nothing that is already so.
# As root, from a checkout of the release:
#
#   sudo sh deploy/bootstrap.sh --deploy-key FILE [--repo URL] -- INSTALL-ARGS...
#
# --deploy-key FILE  the public key the release's deploy job signs in with;
#                    it may run ronote-deploy and nothing else
# --repo URL         where ronote-deploy fetches releases (the GitHub repo)
# INSTALL-ARGS       deploy/install.sh's, with its secrets in the environment
#
#   1. Podman, pasta and the tools the scripts use; unattended upgrades on;
#      podman allowed to stop pasta under Ubuntu's AppArmor profile;
#   2. SSH by key alone, and no root login (where sshd runs);
#   3. 80 and 443 opened in Oracle's iptables rules, which reject them;
#   4. the deploy user, its forced command, and ronote-deploy's bare clone;
#   5. install.sh.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
key=
repo=https://github.com/ROKUMATE/RoNote.git

say() {
	printf 'bootstrap: %s\n' "$*"
}

die() {
	say "$*" >&2
	exit 1
}

while [ $# -gt 0 ]; do
	case $1 in
	--deploy-key) key=${2:?}; shift 2 ;;
	--repo) repo=${2:?}; shift 2 ;;
	--) shift; break ;;
	*) die "unknown argument $1; see the comment at the top of $0" ;;
	esac
done
[ "$(id -u)" -eq 0 ] || die "run it as root, with sudo"
[ -n "$key" ] && [ -f "$key" ] || die "--deploy-key: the deploy job's public key, as a file"
# shellcheck source=/dev/null # the host's own
. /etc/os-release
[ "${ID:-}" = ubuntu ] && [ "${VERSION_ID:-}" = 26.04 ] ||
	die "Ubuntu 26.04 only (this is ${PRETTY_NAME:-unknown}): 24.04's Podman can't run the units (docs/adr/0014-production-topology.md)"

# 1. Packages, and the security updates installing themselves.
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq podman passt uidmap unattended-upgrades git curl openssl >/dev/null
printf 'APT::Periodic::Update-Package-Lists "1";\nAPT::Periodic::Unattended-Upgrade "1";\n' >/etc/apt/apt.conf.d/20auto-upgrades
say "packages, with unattended upgrades on"
sh "$here/pasta-apparmor.sh"

# 2. SSH by key alone. A host without sshd (CI's runner) has nothing to harden.
if command -v sshd >/dev/null 2>&1; then
	install -d /etc/ssh/sshd_config.d
	printf 'PasswordAuthentication no\nKbdInteractiveAuthentication no\nPermitRootLogin no\n' >/etc/ssh/sshd_config.d/10-ronote.conf
	sshd -t || die "sshd refuses its configuration; see above"
	systemctl try-reload-or-restart ssh.service
	say "SSH takes keys alone, and no root"
else
	say "no sshd here: SSH left as it is"
fi

# 3. Oracle's images reject 80 and 443 in iptables, whatever the security
#    list allows; other hosts leave the firewall to their provider.
opened=false
for rules in /etc/iptables/rules.v4 /etc/iptables/rules.v6; do
	[ -f "$rules" ] || continue
	sh "$here/open-web-ports.sh" "$rules"
	opened=true
done
if [ "$opened" = true ]; then
	iptables-restore </etc/iptables/rules.v4
	[ ! -f /etc/iptables/rules.v6 ] || ip6tables-restore </etc/iptables/rules.v6
else
	say "no iptables rules file: the provider's firewall alone decides what reaches 80 and 443"
fi

# 4. The deploy user: its key runs ronote-deploy with the tag it sends, as
#    root by one sudoers line, and nothing else (no shell, no forwarding).
id deploy >/dev/null 2>&1 || useradd --create-home --shell /bin/sh deploy
install -d -o deploy -g deploy -m 700 /home/deploy/.ssh
# shellcheck disable=SC2016 # sshd's shell expands it, with the tag the client sent
printf 'command="sudo -n /usr/local/bin/ronote-deploy \\"$SSH_ORIGINAL_COMMAND\\"",no-pty,no-port-forwarding,no-agent-forwarding,no-X11-forwarding,no-user-rc %s\n' \
	"$(cat "$key")" >/home/deploy/.ssh/authorized_keys
chown deploy:deploy /home/deploy/.ssh/authorized_keys
chmod 600 /home/deploy/.ssh/authorized_keys
printf 'deploy ALL=(root) NOPASSWD: /usr/local/bin/ronote-deploy\n' >/etc/sudoers.d/ronote-deploy.new
chmod 440 /etc/sudoers.d/ronote-deploy.new
visudo -cqf /etc/sudoers.d/ronote-deploy.new || die "visudo refuses the deploy user's sudoers line"
mv /etc/sudoers.d/ronote-deploy.new /etc/sudoers.d/ronote-deploy
install -d /usr/local/lib/ronote /opt/ronote
install -m 755 "$here/ronote-deploy.sh" "$here/as-ronote.sh" /usr/local/lib/ronote/
printf '#!/bin/sh\nexec sh /usr/local/lib/ronote/ronote-deploy.sh "$@"\n' >/usr/local/bin/ronote-deploy
chmod 755 /usr/local/bin/ronote-deploy
[ -d /opt/ronote/repo.git ] || git clone --quiet --bare "$repo" /opt/ronote/repo.git
say "the deploy user may run ronote-deploy, and only that"

# 5. RoNote.
exec sh "$here/install.sh" "$@"
