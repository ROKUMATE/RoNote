#!/bin/sh
# Gives the end-to-end tests an empty, migrated database at
# RONOTE_E2E_DATABASE_URL. Locally it recreates that database in the dev
# stack's Postgres (make dev-up); in CI the job's Postgres service starts
# with it empty.
set -eu

: "${RONOTE_E2E_DATABASE_URL:?RONOTE_E2E_DATABASE_URL is not set}"

if [ -z "${CI:-}" ]; then
  name=${RONOTE_E2E_DATABASE_URL##*/}
  name=${name%%\?*}
  in_postgres() { sh "$(dirname "$0")/container.sh" compose exec -T postgres "$@"; }
  { in_postgres dropdb -U ronote --if-exists --force "$name" && in_postgres createdb -U ronote "$name"; } || {
    echo "e2e-db: cannot recreate $name in the dev stack's Postgres; run make dev-up first" >&2
    exit 1
  }
fi

go tool goose -dir migrations postgres "$RONOTE_E2E_DATABASE_URL" up
