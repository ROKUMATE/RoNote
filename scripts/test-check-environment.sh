#!/bin/sh
# Runs deploy/check-environment.sh on made-up `systemctl --user show-environment`
# output: ronote's manager as it should be, and with another user's
# directories in it. Exits non-zero if any case fails.
set -u

scripts=$(cd "$(dirname "$0")" && pwd)
check=$scripts/../deploy/check-environment.sh
passed=0
failed=0

result() {
  if [ "$2" = ok ]; then
    passed=$((passed + 1))
    printf 'ok    %s\n' "$1"
  else
    failed=$((failed + 1))
    printf 'FAIL  %s (%s)\n' "$1" "$2"
  fi
}

# passes NAME ENV: the check accepts ENV for uid 1001 and home /home/ronote.
passes() {
  if out=$(printf '%s\n' "$2" | sh "$check" 1001 /home/ronote 2>&1); then
    result "$1" ok
  else
    result "$1" "refused: $out"
  fi
}

# refuses NAME ENV WORD...: it refuses ENV and its message has every WORD.
refuses() {
  name=$1
  env=$2
  shift 2
  if out=$(printf '%s\n' "$env" | sh "$check" 1001 /home/ronote 2>&1); then
    result "$name" 'accepted it'
    return
  fi
  for word in "$@"; do
    case $out in
      *"$word"*) ;;
      *) result "$name" "message lacks $word: $out"; return ;;
    esac
  done
  result "$name" ok
}

clean='HOME=/home/ronote
LANG=C.UTF-8
LOGNAME=ronote
PATH=/usr/local/sbin:/usr/local/bin:/usr/bin
USER=ronote
XDG_RUNTIME_DIR=/run/user/1001
DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1001/bus'

passes 'a clean manager passes' "$clean"
passes 'XDG dirs inside the home pass' "$clean
XDG_CONFIG_HOME=/home/ronote/.config
XDG_DATA_HOME=/home/ronote/.local/share
XDG_STATE_HOME=/home/ronote/.local/state
XDG_CACHE_HOME=/home/ronote/.cache"

refuses 'a leaked XDG_CONFIG_HOME is named, with where it comes from' "$clean
XDG_CONFIG_HOME=/home/runner/.config" \
  XDG_CONFIG_HOME /home/runner/.config /etc/environment /etc/security/pam_env.conf
refuses "another user's XDG_RUNTIME_DIR is refused" "HOME=/home/ronote
XDG_RUNTIME_DIR=/run/user/1000" \
  XDG_RUNTIME_DIR /run/user/1000
refuses 'a missing XDG_RUNTIME_DIR is refused' 'HOME=/home/ronote' XDG_RUNTIME_DIR /run/user/1001
refuses 'a directory that only starts like the home is refused' "$clean
XDG_DATA_HOME=/home/ronote2/.local/share" XDG_DATA_HOME
refuses 'every bad variable is named' "$clean
XDG_STATE_HOME=/home/runner/.local/state
XDG_CACHE_HOME=/home/runner/.cache" XDG_STATE_HOME XDG_CACHE_HOME

printf '%s passed, %s failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
