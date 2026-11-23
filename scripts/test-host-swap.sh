#!/bin/sh
# Runs deploy/host-swap.sh on a made-up root: /proc/swaps, /etc/fstab and
# /etc/sysctl.d, with fallocate, mkswap, swapon and sysctl faked on PATH.
# Exits non-zero if any case fails.
set -u

scripts=$(cd "$(dirname "$0")" && pwd)
swap=$scripts/../deploy/host-swap.sh
work=$(mktemp -d "${TMPDIR:-/tmp}/host-swap-test.XXXXXX")
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

# The fakes log each call to $CALLS; swapon adds its file to the root's
# /proc/swaps, as the kernel would.
mkdir "$work/bin"
cat >"$work/bin/fallocate" <<'EOF'
#!/bin/sh
printf 'fallocate %s\n' "$*" >>"$CALLS"
for last; do :; done
: >"$last"
EOF
cat >"$work/bin/mkswap" <<'EOF'
#!/bin/sh
printf 'mkswap %s\n' "$*" >>"$CALLS"
EOF
cat >"$work/bin/swapon" <<'EOF'
#!/bin/sh
printf 'swapon %s\n' "$*" >>"$CALLS"
printf '%s file 2097148 0 -2\n' "${1#"$ROOT"}" >>"$ROOT/proc/swaps"
EOF
cat >"$work/bin/sysctl" <<'EOF'
#!/bin/sh
printf 'sysctl %s\n' "$*" >>"$CALLS"
EOF
chmod +x "$work/bin/"*

# fresh: a root with no swap, an fstab with the root file system.
fresh() {
  root=$work/root
  rm -rf "${root:?}" "$work/calls"
  mkdir -p "$root/proc" "$root/etc/sysctl.d"
  : >"$work/calls"
  printf 'Filename\t\t\t\tType\t\tSize\t\tUsed\t\tPriority\n' >"$root/proc/swaps"
  printf 'LABEL=cloudimg-rootfs\t/\text4\tdiscard,commit=30,errors=remount-ro\t0 1\n' >"$root/etc/fstab"
}

run() {
  out=$(env PATH="$work/bin:$PATH" CALLS="$work/calls" ROOT="$root" sh "$swap" "$root" 2>&1)
}

fresh
run
status=$?
if [ "$status" -eq 0 ] && grep -q "^fallocate -l 2G $root/swapfile$" "$work/calls" && grep -q "^mkswap $root/swapfile$" "$work/calls" &&
  grep -q "^swapon $root/swapfile$" "$work/calls" && [ -n "$(find "$root/swapfile" -perm 600)" ]; then
  result 'a host without swap gets a 2 GiB swap file, readable by root alone' ok
else
  result 'a host without swap gets a 2 GiB swap file, readable by root alone' "exit $status: $out; $(cat "$work/calls")"
fi
if grep -qx '/swapfile none swap sw 0 0' "$root/etc/fstab" && grep -q '^LABEL=cloudimg-rootfs' "$root/etc/fstab"; then
  result 'fstab turns it on at boot, and keeps its lines' ok
else
  result 'fstab turns it on at boot, and keeps its lines' "$(cat "$root/etc/fstab")"
fi
if [ "$(cat "$root/etc/sysctl.d/90-ronote-swap.conf")" = 'vm.swappiness = 10' ] &&
  grep -q "^sysctl -q -p $root/etc/sysctl.d/90-ronote-swap.conf$" "$work/calls"; then
  result 'swappiness is 10, now and at boot' ok
else
  result 'swappiness is 10, now and at boot' "$(cat "$work/calls")"
fi

: >"$work/calls"
cp "$root/etc/fstab" "$work/fstab.once"
run
if [ ! -s "$work/calls" ] && cmp -s "$work/fstab.once" "$root/etc/fstab" &&
  [ "$out" = "host-swap: $root/swapfile is on, with swappiness 10" ]; then
  result 'a second run changes nothing' ok
else
  result 'a second run changes nothing' "said: $out; $(cat "$work/calls")"
fi

fresh
printf '/dev/sdb2 partition 3145724 0 -2\n' >>"$root/proc/swaps"
run
if [ ! -s "$work/calls" ] && [ ! -e "$root/swapfile" ] && [ ! -e "$root/etc/sysctl.d/90-ronote-swap.conf" ] &&
  ! grep -q swap "$root/etc/fstab"; then
  result "a host with its own swap is left alone" ok
else
  result "a host with its own swap is left alone" "said: $out; $(cat "$work/calls")"
fi

# Made, then the host rebooted before fstab had it, or a run stopped half way.
fresh
: >"$root/swapfile"
printf '/swapfile none swap sw 0 0\n' >>"$root/etc/fstab"
run
if grep -q "^mkswap $root/swapfile$" "$work/calls" && grep -q "^swapon $root/swapfile$" "$work/calls" &&
  [ "$(grep -c swapfile "$root/etc/fstab")" = 1 ]; then
  result 'a swap file that is off is made again and turned on, fstab unchanged' ok
else
  result 'a swap file that is off is made again and turned on, fstab unchanged' "said: $out; $(cat "$work/calls"); $(cat "$root/etc/fstab")"
fi

fresh
rm "$work/bin/fallocate"
printf '#!/bin/sh\nexit 1\n' >"$work/bin/fallocate"
cat >"$work/bin/dd" <<'EOF'
#!/bin/sh
printf 'dd %s\n' "$*" >>"$CALLS"
for a; do case $a in of=*) : >"${a#of=}" ;; esac; done
EOF
chmod +x "$work/bin/fallocate" "$work/bin/dd"
run
if grep -q "^dd if=/dev/zero of=$root/swapfile bs=1M count=2048$" "$work/calls" && grep -q "^swapon $root/swapfile$" "$work/calls"; then
  result 'a file system fallocate refuses gets the file written by dd' ok
else
  result 'a file system fallocate refuses gets the file written by dd' "said: $out; $(cat "$work/calls")"
fi

printf '%s passed, %s failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
