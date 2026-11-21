#!/bin/sh
# Installs RoNote's units on one Linux host (docs/adr/0014-production-topology.md),
# as root: the ronote user, rootless and lingering; unprivileged ports from
# 80; the Quadlet units; the podman secrets; the embedding model; then it
# starts everything and waits until it is healthy. Running it again keeps
# the user, the secrets and the data, and installs the units again.
#
#   sudo sh deploy/install.sh --version v1.2.3 --config DIR
#       [--drop-ins DIR] [--images FILE] [--skip-model]
#
# --config DIR   files for ~ronote/.config/ronote, ronote.env at least
#                (deploy/ronote.env.example); kept from an earlier run if absent
# --drop-ins DIR units and drop-ins added beside the units (deploy/quadlet/ci)
# --images FILE  an image archive (podman save) to load instead of pulling
# --skip-model   no embedding model: the drop-ins replace Ollama
#
# Secrets it doesn't generate come from its environment, never from a file:
# RONOTE_GITHUB_CLIENT_SECRET, and RONOTE_ANTHROPIC_API_KEY when chat is on;
# RONOTE_BLOB_ACCESS_KEY and RONOTE_BLOB_SECRET_KEY for an external S3.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
repo=$(dirname "$here")
user=ronote
version=
config=
dropins=
images=
skip_model=false

die() {
	printf 'install: %s\n' "$*" >&2
	exit 1
}

say() {
	printf 'install: %s\n' "$*"
}

while [ $# -gt 0 ]; do
	case $1 in
	--version) version=${2:?}; shift 2 ;;
	--config) config=${2:?}; shift 2 ;;
	--drop-ins) dropins=${2:?}; shift 2 ;;
	--images) images=${2:?}; shift 2 ;;
	--skip-model) skip_model=true; shift ;;
	*) die "unknown argument $1; see the comment at the top of $0" ;;
	esac
done
[ "$(id -u)" -eq 0 ] || die "run it as root, with sudo"
[ -n "$version" ] || die "--version is required: the images' tag, such as v1.2.3"

# Quadlet takes Entrypoint= (pdftext's) from Podman 5.0 on.
podman=$(podman version --format '{{.Client.Version}}') || die "no podman: install Podman 5.0 or later"
IFS=. read -r major _ <<EOF
$podman
EOF
[ "$major" -ge 5 ] || die "Podman $podman is too old: the units need 5.0 or later, for Quadlet's Entrypoint="

# The user, with its own subordinate IDs for rootless containers, and its
# services running without a login.
if ! id "$user" >/dev/null 2>&1; then
	useradd --create-home --shell /usr/sbin/nologin "$user"
	say "made the user $user"
fi
grep -q "^$user:" /etc/subuid || usermod --add-subuids 200000-265535 --add-subgids 200000-265535 "$user"
uid=$(id -u "$user")
home=$(getent passwd "$user" | cut -d: -f6)
loginctl enable-linger "$user"
tries=0
until [ -S "/run/user/$uid/bus" ]; do
	tries=$((tries + 1))
	[ "$tries" -le 30 ] || die "$user's systemd never started"
	sleep 1
done

# Caddy, as that user, binds 80 and 443.
printf 'net.ipv4.ip_unprivileged_port_start=80\n' >/etc/sysctl.d/90-ronote.conf
sysctl -q -p /etc/sysctl.d/90-ronote.conf

as_user() {
	sh "$here/as-ronote.sh" "$@"
}

# ronote's manager gets its environment from the host's PAM, which can hold
# another user's directories (GitHub's runner image puts its own user's in
# /etc/environment). Quadlet and podman would then use those, so this stops
# the install instead of overriding them.
manager_env=$(as_user systemctl --user show-environment) || die "can't read $user's systemd environment"
printf '%s\n' "$manager_env" | sh "$here/check-environment.sh" "$uid" "$home" || die "$user's systemd has another user's directories; see above"

# Configuration, then the units.
etc=$home/.config/ronote
units=$home/.config/containers/systemd
install -d -o "$user" -g "$user" -m 700 "$home/.config" "$etc" "$home/.config/containers" "$units"
if [ -n "$config" ]; then
	cp -R "$config"/. "$etc"/
fi
[ -f "$etc/ronote.env" ] || die "no $etc/ronote.env: pass --config with one (deploy/ronote.env.example)"
cp "$repo/deploy/pdftext/pdftext.py" "$etc/pdftext.py"
chown -R "$user:$user" "$etc"

# An external S3 (R2, B2) needs no SeaweedFS.
seaweedfs=true
grep -q '^RONOTE_BLOB_ENDPOINT=http://ronote-seaweedfs:' "$etc/ronote.env" || seaweedfs=false
set -- "$units" "$version"
[ "$seaweedfs" = true ] || set -- "$@" --no-seaweedfs
[ -z "$dropins" ] || set -- "$@" --drop-ins "$dropins"

find "$units" -mindepth 1 -maxdepth 1 -name 'ronote-*' -exec rm -rf {} +
sh "$here/units.sh" "$@"
chown -R "$user:$user" "$units"

# Secrets: made once, never printed, kept by later runs.
secret() {
	name=$1
	value=$2
	if as_user podman secret exists "$name"; then
		return
	fi
	[ -n "$value" ] || die "secret $name: set its variable in this command's environment"
	printf '%s' "$value" | as_user podman secret create "$name" - >/dev/null
	say "made the secret $name"
}
random() {
	od -An -N24 -tx1 /dev/urandom | tr -d ' \n'
}
existing() {
	as_user podman secret exists "$1" && printf 'kept'
}
if [ -z "$(existing ronote-postgres-password)" ]; then
	password=$(random)
	secret ronote-postgres-password "$password"
	as_user podman secret rm ronote-database-url >/dev/null 2>&1 || true
	secret ronote-database-url "postgres://ronote:$password@ronote-postgres:5432/ronote?sslmode=disable"
fi
secret ronote-github-client-secret "${RONOTE_GITHUB_CLIENT_SECRET:-}"
# Chat off needs no key, but the units name the secret.
secret ronote-anthropic-api-key "${RONOTE_ANTHROPIC_API_KEY:-none}"
secret ronote-docling-api-key "$(random)"
secret ronote-pdftext-api-key "$(random)"
if [ "$seaweedfs" = true ] && [ -z "$(existing ronote-blob-secret-key)" ]; then
	access=$(random)
	key=$(random)
	secret ronote-blob-access-key "$access"
	secret ronote-blob-secret-key "$key"
	as_user podman secret rm ronote-seaweedfs-s3 >/dev/null 2>&1 || true
	secret ronote-seaweedfs-s3 "$(printf '{"identities":[{"name":"ronote","credentials":[{"accessKey":"%s","secretKey":"%s"}],"actions":["Admin","Read","Write","List","Tagging"]}]}' "$access" "$key")"
else
	secret ronote-blob-access-key "${RONOTE_BLOB_ACCESS_KEY:-}"
	secret ronote-blob-secret-key "${RONOTE_BLOB_SECRET_KEY:-}"
fi

# Root opens the archive, which may sit where ronote can't read, such as a
# checkout; ronote's podman reads it from stdin.
if [ -n "$images" ]; then
	as_user podman load -q <"$images"
fi

as_user systemctl --user daemon-reload

# Quadlet's generator makes the services at daemon-reload, and leaves out,
# with no more than a log line, a unit it can't convert or one in a
# directory it doesn't look in. So each one is checked here, and a missing
# one stops the install with what Quadlet and systemd say, not at restart.
want=
for f in "$units"/ronote-*.container "$units"/ronote-*.network "$units"/ronote-*.volume; do
	name=$(basename "$f")
	case $name in
	*.container) want="$want ${name%.container}.service" ;;
	*.network) want="$want ${name%.network}-network.service" ;;
	*.volume) want="$want ${name%.volume}-volume.service" ;;
	esac
done
made=$(as_user systemctl --user list-unit-files --no-legend 'ronote-*' | awk '{ print $1 }')
missing=
for s in $want; do
	printf '%s\n' "$made" | grep -qx "$s" || missing="$missing $s"
done
if [ -n "$missing" ]; then
	generator=/usr/lib/systemd/user-generators/podman-user-generator
	[ -e "$generator" ] || generator=/usr/libexec/podman/quadlet
	say "Quadlet's dry run on $units, as $user:"
	{ as_user env QUADLET_UNIT_DIRS="$units" "$generator" -dryrun -user >/dev/null; } 2>&1 || true
	say "versions:"
	as_user podman --version || true
	as_user systemctl --version || true
	say "the user generators:"
	ls -l /usr/lib/systemd/user-generators/ || true
	say "$user's systemd environment, which the generator runs in:"
	as_user systemctl --user show-environment || true
	say "the generator's log:"
	as_user journalctl --user -b --no-pager _COMM=podman-user-gen || true
	die "Quadlet made no unit for:$missing; the reasons are above"
fi

# The embedding model, pulled once over egress into Ollama's volume and
# checked against the pin; Ollama itself then runs with no way out.
if [ "$skip_model" = false ]; then
	model=$(sed -n 's/^pinned_model=//p' "$repo/scripts/embed-check.sh")
	digest=$(sed -n 's/^pinned_digest=//p' "$repo/scripts/embed-check.sh")
	image=$(sed -n 's/^Image=//p' "$units/ronote-ollama.container")
	as_user systemctl --user start ronote-egress-network.service ronote-ollama-volume.service
	as_user podman rm -f ronote-ollama-pull >/dev/null 2>&1 || true
	as_user podman run -d --name ronote-ollama-pull --network ronote-egress \
		-v ronote-ollama:/root/.ollama "$image" >/dev/null
	tries=0
	until as_user podman exec ronote-ollama-pull ollama list >/dev/null 2>&1; do
		tries=$((tries + 1))
		[ "$tries" -le 30 ] || die "Ollama didn't start to pull the model"
		sleep 1
	done
	as_user podman exec ronote-ollama-pull ollama pull "$model" >/dev/null
	got=$(as_user podman exec ronote-ollama-pull ollama list | awk -v m="$model" '$1 == m { print $2 }')
	as_user podman rm -f ronote-ollama-pull >/dev/null
	[ "$got" = "$digest" ] || die "pulled $model is $got, not the pinned $digest"
	say "pulled $model ($digest)"
fi

# Everything, in the units' order, restarted so a new install takes effect;
# systemctl returns once each is healthy.
services=
for f in "$units"/ronote-*.container; do
	services="$services $(basename "$f" .container).service"
done
# shellcheck disable=SC2086 # one word per service
as_user systemctl --user restart $services
say "started:$services"
