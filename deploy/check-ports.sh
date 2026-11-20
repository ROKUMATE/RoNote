#!/bin/sh
# Checks that only Caddy answers from outside the containers' networks
# (docs/adr/0014-production-topology.md): the ronote user's processes listen
# on 80 and 443 alone, and both answer here. At each address given, every
# service's own port must also refuse a connection; give none where other
# software publishes those ports, as the dev stack does in the podman VM.
#
#   sh deploy/check-ports.sh [ADDRESS...]
set -u

failed=0
fail() {
	printf 'check-ports: %s\n' "$*" >&2
	failed=1
}

# curl's 7 is a refused connection and 28 one that never answered; anything
# else connected, if only to fail TLS or the request.
answers() {
	curl -s -o /dev/null --connect-timeout 3 --max-time 5 "http://$1:$2/"
	case $? in 7 | 28) return 1 ;; *) return 0 ;; esac
}

uid=$(id -u ronote) || exit 2
listening=$(ss -H -l -n -t -u -e | awk -v uid="uid:$uid" '
	index($0, uid) { n = split($5, at, ":"); print at[n] }' | sort -un | tr '\n' ' ')
[ "$listening" = "80 443 " ] || fail "ronote listens on ${listening:-nothing}; want 80 and 443 alone"

for port in 80 443; do
	answers 127.0.0.1 "$port" || fail "127.0.0.1:$port refuses; Caddy should answer"
done
for addr in "$@"; do
	for port in 5001 5002 5432 8080 8081 8090 8094 8333 8443 11434; do
		if answers "$addr" "$port"; then
			fail "$addr:$port answers; only 80 and 443 may"
		fi
	done
done
[ "$failed" -eq 0 ] && printf 'check-ports: ronote listens on 80 and 443 alone%s\n' "${*:+; nothing else answers at $*}"
exit "$failed"
