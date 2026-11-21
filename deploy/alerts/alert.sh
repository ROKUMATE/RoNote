#!/bin/sh
# Sends one alert to ntfy (docs/adr/0015-operations.md), as ronote:
#
#   sh alert.sh TITLE MESSAGE
#
# to $RONOTE_NTFY_URL/<topic>, the topic being the podman secret
# ronote-ntfy-topic. Its name is all anyone needs to read or post, so it is
# never on a command line or in output: curl reads the URL from stdin.
# Without RONOTE_NTFY_URL (CI) it only says what it would have sent.
set -eu

title=${1:?a title}
message=${2:?a message}
etc=${RONOTE_ETC:-$HOME/.config/ronote}
set -a
# shellcheck source=/dev/null # the deployment's own settings
. "$etc/ronote.env"
set +a

if [ -z "${RONOTE_NTFY_URL:-}" ]; then
	printf 'alert (not sent, RONOTE_NTFY_URL is unset): %s: %s\n' "$title" "$message"
	exit 0
fi
topic=$(podman secret inspect --showsecret --format '{{.SecretData}}' ronote-ntfy-topic)
printf 'url = "%s/%s"\n' "${RONOTE_NTFY_URL%/}" "$topic" |
	curl -fsS --max-time 20 -K - -o /dev/null -H "Title: RoNote: $title" -H "Tags: warning" --data-binary "$message" ||
	{ printf 'alert: ntfy refused or is unreachable: %s\n' "$title" >&2; exit 1; }
printf 'alert sent: %s\n' "$title"
