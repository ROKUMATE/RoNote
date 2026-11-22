#!/bin/sh
# Runs deploy/pasta-apparmor.sh on copies of Ubuntu 26.04's pasta profile
# (deploy/testdata/usr.bin.pasta), with apparmor_parser and aa-enabled faked
# on PATH. Exits non-zero if any case fails.
set -u

scripts=$(cd "$(dirname "$0")" && pwd)
testdata=$scripts/../deploy/testdata
fix=$scripts/../deploy/pasta-apparmor.sh
work=$(mktemp -d "${TMPDIR:-/tmp}/pasta-apparmor-test.XXXXXX")
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

# The fakes log each call's arguments; PARSER_FAILS and APPARMOR_OFF make
# them fail.
mkdir "$work/bin"
cat >"$work/bin/apparmor_parser" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >>"$CALLS"
[ -z "${PARSER_FAILS:-}" ]
EOF
cat >"$work/bin/aa-enabled" <<'EOF'
#!/bin/sh
[ -z "${APPARMOR_OFF:-}" ]
EOF
chmod +x "$work/bin/apparmor_parser" "$work/bin/aa-enabled"

# fresh: a profile directory with the package's profile and abstraction.
fresh() {
  rm -rf "${work:?}/etc" "$work/calls"
  mkdir -p "$work/etc/abstractions"
  : >"$work/calls"
  cp "$testdata/usr.bin.pasta" "$work/etc/usr.bin.pasta"
  printf '  include <abstractions/passt>\n' >"$work/etc/abstractions/pasta"
}

# after_include LINE: the package's profile with LINE after its include.
after_include() {
  awk -v line="$1" '{ print } /^  include <abstractions\/pasta>$/ { print line }' "$testdata/usr.bin.pasta"
}

# run [VAR=value...]: the script on the directory, with the fakes; its output in $out.
run() {
  out=$(env PATH="$work/bin:$PATH" CALLS="$work/calls" "$@" sh "$fix" "$work/etc" 2>&1)
}

fresh
run
if cmp -s "$testdata/usr.bin.pasta.fixed" "$work/etc/usr.bin.pasta"; then
  result 'it adds exactly the upstream lines, where the patch puts them' ok
else
  result 'it adds exactly the upstream lines, where the patch puts them' "$(diff "$testdata/usr.bin.pasta.fixed" "$work/etc/usr.bin.pasta")"
fi
if [ "$(cat "$work/calls")" = "-r $work/etc/usr.bin.pasta" ]; then
  result 'it reloads that one profile' ok
else
  result 'it reloads that one profile' "apparmor_parser got: $(cat "$work/calls")"
fi

: >"$work/calls"
run
if cmp -s "$testdata/usr.bin.pasta.fixed" "$work/etc/usr.bin.pasta" && [ ! -s "$work/calls" ] &&
  [ "$out" = "pasta-apparmor: $work/etc/usr.bin.pasta already lets podman signal pasta" ]; then
  result 'a second run changes and reloads nothing' ok
else
  result 'a second run changes and reloads nothing' "said: $out; apparmor_parser got: $(cat "$work/calls")"
fi

fresh
printf '  signal (receive) peer=podman,\n' >>"$work/etc/abstractions/pasta"
cp "$work/etc/usr.bin.pasta" "$work/before"
run
if cmp -s "$work/before" "$work/etc/usr.bin.pasta" && [ ! -s "$work/calls" ]; then
  result "a profile fixed in pasta's abstraction is left alone" ok
else
  result "a profile fixed in pasta's abstraction is left alone" "said: $out"
fi

fresh
after_include '  signal (receive, send) peer=podman,	# as Ubuntu might ship it' >"$work/shipped"
cp "$work/shipped" "$work/etc/usr.bin.pasta"
run
if cmp -s "$work/shipped" "$work/etc/usr.bin.pasta" && [ ! -s "$work/calls" ]; then
  result 'a profile shipped with a signal rule for podman is left alone' ok
else
  result 'a profile shipped with a signal rule for podman is left alone' "said: $out"
fi

fresh
after_include '  # signal (receive) peer=podman,' >"$work/commented"
cp "$work/commented" "$work/etc/usr.bin.pasta"
run
if [ -s "$work/calls" ] && grep -q '^  signal (receive) peer=podman,$' "$work/etc/usr.bin.pasta"; then
  result 'a commented-out rule is no rule' ok
else
  result 'a commented-out rule is no rule' "said: $out"
fi

fresh
run PARSER_FAILS=1
status=$?
if cmp -s "$testdata/usr.bin.pasta" "$work/etc/usr.bin.pasta" && [ "$status" -ne 0 ] 2>/dev/null; then
  result 'a profile the parser refuses is put back as it was' ok
else
  result 'a profile the parser refuses is put back as it was' "exit $status, said: $out"
fi

fresh
run APPARMOR_OFF=1
if cmp -s "$testdata/usr.bin.pasta.fixed" "$work/etc/usr.bin.pasta" && [ ! -s "$work/calls" ]; then
  result 'with AppArmor off it adds the rule and loads nothing' ok
else
  result 'with AppArmor off it adds the rule and loads nothing' "said: $out"
fi

fresh
printf 'profile pasta /usr/bin/pasta {\n}\n' >"$work/etc/usr.bin.pasta"
cp "$work/etc/usr.bin.pasta" "$work/before"
if ! run && cmp -s "$work/before" "$work/etc/usr.bin.pasta"; then
  result 'a profile of another shape fails, unchanged' ok
else
  result 'a profile of another shape fails, unchanged' "said: $out"
fi

fresh
rm "$work/etc/usr.bin.pasta"
if run && [ ! -s "$work/calls" ]; then
  result 'no pasta profile, nothing to do' ok
else
  result 'no pasta profile, nothing to do' "said: $out"
fi

printf '%s passed, %s failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
