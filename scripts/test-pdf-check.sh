#!/bin/sh
# Runs scripts/pdf-check.sh against a fake curl, which answers each service
# as a variable the test sets says. Exits non-zero if any case fails.
set -u

scripts=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/pdf-check-test.XXXXXX")
trap 'rm -rf "$work"' EXIT
passed=0
failed=0

# The fake: the last argument is the URL. The bucket's HEAD prints BUCKET
# as its status (000 when nothing answers); /health succeeds when DOCLING
# or PDFTEXT, by port, is "up".
bin=$work/bin
mkdir -p "$bin"
cat >"$bin/curl" <<'FAKE'
#!/bin/sh
for last; do :; done
case $last in
  *:8333/*) printf '%s' "$BUCKET" ;;
  *:5001/health) [ "$DOCLING" = up ] ;;
  *:5002/health) [ "$PDFTEXT" = up ] ;;
  *) exit 7 ;;
esac
FAKE
chmod +x "$bin/curl"

# expect NAME BUCKET DOCLING PDFTEXT STATUS TEXT: the check exits with
# STATUS and prints TEXT.
expect() {
  BUCKET=$2 DOCLING=$3 PDFTEXT=$4 PATH="$bin:$PATH" sh "$scripts/pdf-check.sh" >"$work/out" 2>&1
  status=$?
  if [ "$status" -eq "$5" ] && grep -q "$6" "$work/out"; then
    passed=$((passed + 1))
    echo "ok    $1"
  else
    failed=$((failed + 1))
    echo "FAIL  $1: exit $status, said: $(cat "$work/out")"
  fi
}

expect "everything answers" 403 up up 0 "the blob store, docling and pdftext answer"
expect "a bucket anyone may read" 200 up up 0 "answer"
expect "no blob store" 000 up up 1 "no blob store answers"
expect "no bucket" 404 up up 1 "has no bucket ronote"
expect "no docling" 403 down up 1 "docling doesn't answer"
expect "no pdftext" 403 up down 1 "pdftext doesn't answer"

echo
echo "$passed passed, $failed failed"
[ "$failed" -eq 0 ]
