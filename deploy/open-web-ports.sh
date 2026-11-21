#!/bin/sh
# Opens 80 and 443 in an iptables-persistent rules file (docs/adr/0015-operations.md):
#
#   sh deploy/open-web-ports.sh /etc/iptables/rules.v4
#
# Oracle's Ubuntu images end their INPUT chain with a catch-all REJECT, which
# blocks the web even when the cloud's security list allows it. The rules go
# just before it; a file that has them, or rejects nothing, is left alone.
# It prints whether it changed the file, and bootstrap.sh then loads it.
set -eu

file=${1:?the rules file, such as /etc/iptables/rules.v4}
if grep -q -- '--dport 443 -j ACCEPT' "$file"; then
	echo "open-web-ports: $file already lets 80 and 443 in"
	exit 0
fi
if ! grep -q '^-A INPUT -j REJECT' "$file"; then
	echo "open-web-ports: $file rejects nothing in INPUT; left alone"
	exit 0
fi
tmp=$file.ronote
awk '
	!done && /^-A INPUT -j REJECT/ {
		print "-A INPUT -p tcp -m state --state NEW -m tcp --dport 80 -j ACCEPT"
		print "-A INPUT -p tcp -m state --state NEW -m tcp --dport 443 -j ACCEPT"
		print "-A INPUT -p udp -m udp --dport 443 -j ACCEPT"
		done = 1
	}
	{ print }
' "$file" >"$tmp"
chmod --reference="$file" "$tmp" 2>/dev/null || chmod 644 "$tmp"
mv "$tmp" "$file"
echo "open-web-ports: $file now lets 80 and 443 (tcp, and udp for HTTP/3) in, before its REJECT"
