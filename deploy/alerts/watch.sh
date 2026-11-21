#!/bin/sh
# RoNote's health, every 5 minutes from its timer, as ronote
# (docs/adr/0015-operations.md). It alerts through alert.sh when:
#
#   the disk is over 80% full;
#   a unit isn't active, or its container is unhealthy, for 5 minutes;
#   the app's certificate expires within 14 days;
#   the api logged that chat reached its daily spend cap.
#
# Each condition alerts once, then again a day later while it lasts; one
# that clears is forgotten, so it alerts afresh if it comes back.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
etc=${RONOTE_ETC:-$HOME/.config/ronote}
units=$HOME/.config/containers/systemd
state=${RONOTE_WATCH_STATE:-$HOME/.local/state/ronote/watch}
mkdir -p "$state"
set -a
# shellcheck source=/dev/null # the deployment's own settings
. "$etc/ronote.env"
set +a
now=$(date +%s)
grace=300
day=86400

# condition KEY BAD WAIT TITLE MESSAGE: BAD is true or false now; after WAIT
# seconds of it, alert, then not again for a day.
condition() {
	key=$1 bad=$2 wait=$3
	if [ "$bad" = false ]; then
		rm -f "$state/$key.since" "$state/$key.sent"
		return
	fi
	[ -f "$state/$key.since" ] || printf '%s' "$now" >"$state/$key.since"
	since=$(cat "$state/$key.since")
	[ $((now - since)) -ge "$wait" ] || return 0
	if [ -f "$state/$key.sent" ] && [ $((now - $(cat "$state/$key.sent"))) -lt "$day" ]; then
		return
	fi
	sh "$here/alert.sh" "$4" "$5" && printf '%s' "$now" >"$state/$key.sent"
}

used=$(df -P "$HOME" | awk 'NR == 2 { sub("%", "", $5); print $5 }')
full=false
[ "$used" -le 80 ] || full=true
condition disk "$full" 0 "the disk is ${used}% full" "Over 80%: free some, or grow the volume."

for f in "$units"/ronote-*.container; do
	[ -e "$f" ] || continue
	name=$(basename "$f" .container)
	down=false
	if ! systemctl --user is-active --quiet "$name.service"; then
		down=true
	elif [ "$(podman inspect --format '{{.State.Health.Status}}' "$name" 2>/dev/null || true)" = unhealthy ]; then
		down=true
	fi
	condition "unit-$name" "$down" "$grace" "$name is down" "Not active, or unhealthy, for 5 minutes: sudo sh deploy/as-ronote.sh systemctl --user status $name"
done

host=${RONOTE_PUBLIC_URL#https://}
expiring=false
if ! printf '' | openssl s_client -connect 127.0.0.1:443 -servername "$host" 2>/dev/null |
	openssl x509 -noout -checkend $((14 * day)) >/dev/null 2>&1; then
	expiring=true
fi
condition certificate "$expiring" 0 "$host's certificate expires within 14 days" "Caddy renews it: see why not in journalctl --user -u ronote-caddy"

# The api logs it once a UTC day (chat.SpendCapReached); what it logged
# since the last look, so one day's line is one alert.
checked=$(cat "$state/cap.checked" 2>/dev/null || printf '%s' "$now")
capped=false
if journalctl --user -u ronote-api.service --since "@$checked" --no-pager -o cat 2>/dev/null | grep -q 'chat spend cap reached'; then
	capped=true
fi
printf '%s' "$now" >"$state/cap.checked"
condition spend-cap "$capped" 0 "chat reached today's spend cap" "Chat is off for everyone until midnight UTC; RONOTE_CHAT_DAILY_USD sets the cap."
