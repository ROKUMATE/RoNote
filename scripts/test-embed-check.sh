#!/bin/sh
# Runs scripts/embed-check.sh against a fake curl, which answers each URL
# from a variable the test sets. Exits non-zero if any case fails.
set -u

scripts=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/embed-check-test.XXXXXX")
trap 'rm -rf "$work"' EXIT
passed=0
failed=0

# The fake: the last argument is the URL. /api/tags prints TAGS; an empty
# variable is a failed request.
bin=$work/bin
mkdir -p "$bin"
cat >"$bin/curl" <<'FAKE'
#!/bin/sh
for last; do :; done
case $last in
  */api/tags) [ -n "$TAGS" ] && printf '%s' "$TAGS" ;;
  *) exit 7 ;;
esac
FAKE
chmod +x "$bin/curl"

tags() {
  printf '{"models":[{"name":"other:1b","digest":"0000"},{"name":"qwen3-embedding:0.6b","model":"qwen3-embedding:0.6b","digest":"%s","details":{"format":"gguf"}}]}' "$1"
}

# expect NAME MODEL TAGS STATUS TEXT...: the check exits with STATUS and
# prints every TEXT.
expect() {
  name=$1
  RONOTE_EMBED_MODEL=$2 TAGS=$3 PATH="$bin:$PATH" sh "$scripts/embed-check.sh" >"$work/out" 2>&1
  status=$?
  want=$4
  shift 4
  out=$(cat "$work/out")
  missing=
  for text in "$@"; do
    case $out in
      *"$text"*) ;;
      *) missing="$missing [$text]" ;;
    esac
  done
  if [ "$status" -eq "$want" ] && [ -z "$missing" ]; then
    passed=$((passed + 1))
    printf 'ok    %s\n' "$name"
  else
    failed=$((failed + 1))
    printf 'FAIL  %s (exit %s, want %s; missing%s)\n' "$name" "$status" "$want" "${missing:- nothing}"
    sed 's/^/      out: /' "$work/out"
  fi
}

export RONOTE_EMBED_URL=http://127.0.0.1:11434
expect 'pinned model pulled' qwen3-embedding:0.6b "$(tags ac6da0dfba84a81fdbfb)" 0 'answers at'
expect 'nothing answers' qwen3-embedding:0.6b '' 1 'no embedder answers' 'ollama serve'
expect 'model not pulled' qwen3-embedding:0.6b '{"models":[]}' 1 "doesn't serve qwen3-embedding:0.6b" 'ollama pull qwen3-embedding:0.6b'
expect 'another build pulled' qwen3-embedding:0.6b "$(tags 1234567890ab)" 1 'not the pinned ac6da0dfba84' 'ollama pull'
expect 'another model, no pin' fake-words '{"models":[{"name":"fake-words","model":"fake-words"}]}' 0 'answers at'

printf '%s passed, %s failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
