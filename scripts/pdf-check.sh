#!/bin/sh
# Checks what PDF imports need (docs/adr/0013-pdf-import.md): the blob store
# answers at RONOTE_BLOB_ENDPOINT and has RONOTE_BLOB_BUCKET, and docling
# and pdftext answer at RONOTE_DOCLING_URL and RONOTE_PDFTEXT_URL with their
# keys: the real ones (make dev-up-docling) or the fake (make run-docling).
set -u

endpoint=${RONOTE_BLOB_ENDPOINT:-http://127.0.0.1:8333}
bucket=${RONOTE_BLOB_BUCKET:-ronote}
docling=${RONOTE_DOCLING_URL:-http://127.0.0.1:5001}
docling_key=${RONOTE_DOCLING_API_KEY:-ronote-dev-docling}
pdftext=${RONOTE_PDFTEXT_URL:-http://127.0.0.1:5002}
pdftext_key=${RONOTE_PDFTEXT_API_KEY:-ronote-dev-pdftext}

die() {
  printf 'doctor: %s\n' "$*" >&2
  exit 1
}

# An anonymous HEAD of the bucket: 403 or 200 when it exists, 404 when not.
status=$(curl -s -o /dev/null -m 5 -w '%{http_code}' -I "$endpoint/$bucket")
case $status in
  200 | 403) ;;
  404) die "the blob store at $endpoint has no bucket $bucket. Run make dev-up, which makes it." ;;
  *) die "no blob store answers at $endpoint. Run make dev-up." ;;
esac
curl -sf -m 5 -H "X-Api-Key: $docling_key" "$docling/health" >/dev/null ||
  die "docling doesn't answer at $docling. Run make dev-up-docling for the real one, or make run-docling for the fake."
curl -sf -m 5 -H "X-Api-Key: $pdftext_key" "$pdftext/health" >/dev/null ||
  die "pdftext doesn't answer at $pdftext. Run make dev-up-docling for the real one, or make run-docling for the fake."
echo "doctor: the blob store, docling and pdftext answer"
