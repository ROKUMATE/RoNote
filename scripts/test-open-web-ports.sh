#!/bin/sh
# Runs deploy/open-web-ports.sh on a copy of the rules file Oracle's Ubuntu
# images ship (deploy/testdata/oracle-rules.v4). Exits non-zero if any case
# fails.
set -u

scripts=$(cd "$(dirname "$0")" && pwd)
open=$scripts/../deploy/open-web-ports.sh
work=$(mktemp -d "${TMPDIR:-/tmp}/open-web-ports-test.XXXXXX")
trap 'rm -rf "$work"' EXIT
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

# line PATTERN: the number of the file's first line matching it.
line() {
  grep -n -- "$1" "$work/rules.v4" | head -n 1 | cut -d: -f1
}

cp "$scripts/../deploy/testdata/oracle-rules.v4" "$work/rules.v4"
sh "$open" "$work/rules.v4" >/dev/null
reject=$(line '^-A INPUT -j REJECT')
for rule in 'tcp --dport 80 -j ACCEPT' 'tcp --dport 443 -j ACCEPT' 'udp --dport 443 -j ACCEPT'; do
  at=$(line "$rule")
  if [ -n "$at" ] && [ "$at" -lt "$reject" ]; then
    result "$rule comes before the REJECT" ok
  else
    result "$rule comes before the REJECT" "at line ${at:-none}, the REJECT at $reject"
  fi
done
# Its 11 rules, and the 3 added.
if grep -q 'InstanceServices -d 169.254.169.254/32' "$work/rules.v4" && [ "$(grep -c '^-A ' "$work/rules.v4")" -eq 14 ]; then
  result 'every rule of the image stays' ok
else
  result 'every rule of the image stays' "$(cat "$work/rules.v4")"
fi

cp "$work/rules.v4" "$work/once"
out=$(sh "$open" "$work/rules.v4")
if cmp -s "$work/once" "$work/rules.v4" && [ "$out" = "open-web-ports: $work/rules.v4 already lets 80 and 443 in" ]; then
  result 'a second run changes nothing' ok
else
  result 'a second run changes nothing' "said: $out"
fi

printf '*filter\n:INPUT ACCEPT [0:0]\nCOMMIT\n' >"$work/open.v4"
cp "$work/open.v4" "$work/open.before"
sh "$open" "$work/open.v4" >/dev/null
if cmp -s "$work/open.before" "$work/open.v4"; then
  result 'a file that rejects nothing is left alone' ok
else
  result 'a file that rejects nothing is left alone' "$(cat "$work/open.v4")"
fi

printf '%s passed, %s failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
