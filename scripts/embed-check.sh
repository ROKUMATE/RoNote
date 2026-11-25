#!/bin/sh
# Checks the embedder search needs (docs/adr/0011-retrieval.md): something
# answers at RONOTE_EMBED_URL, it serves RONOTE_EMBED_MODEL, and when that is
# the pinned Ollama model, the build pulled is the pinned one.
set -u

url=${RONOTE_EMBED_URL:-http://127.0.0.1:11434}
model=${RONOTE_EMBED_MODEL:-qwen3-embedding:0.6b}
pinned_model=qwen3-embedding:0.6b
pinned_digest=ac6da0dfba84

die() {
  printf 'doctor: %s\n' "$*" >&2
  exit 1
}

# Ollama lists each model at /api/tags, with its digest.
tags=$(curl -sf -m 5 "$url/api/tags") ||
  die "no embedder answers at $url. Start Ollama (ollama serve), or set RONOTE_EMBED_URL."
case $tags in
  *"\"name\":\"$model\""*) ;;
  *) die "the embedder at $url doesn't serve $model. With Ollama: ollama pull $model" ;;
esac

if [ "$model" = "$pinned_model" ]; then
  digest=$(printf '%s' "$tags" | tr '{' '\n' | grep "\"name\":\"$model\"" |
    sed -n 's/.*"digest":"\([0-9a-f]*\)".*/\1/p' | head -n 1)
  case $digest in
    "$pinned_digest"*) ;;
    *) die "$model is build ${digest:-unknown}, not the pinned $pinned_digest. Run: ollama pull $model" ;;
  esac
fi
echo "doctor: $model answers at $url"
