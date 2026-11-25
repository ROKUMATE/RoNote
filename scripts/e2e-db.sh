#!/bin/sh
# Gives the end-to-end tests an empty, migrated database at
# RONOTE_E2E_DATABASE_URL. Locally it recreates that database in the
# Postgres of the compose project RONOTE_E2E_PROJECT, the tests' own (make
# e2e-stack-up), never the dev stack's; in CI the job's Postgres service
# starts with it empty.
set -eu

: "${RONOTE_E2E_DATABASE_URL:?RONOTE_E2E_DATABASE_URL is not set}"

if [ -z "${CI:-}" ]; then
  name=${RONOTE_E2E_DATABASE_URL##*/}
  name=${name%%\?*}
  project=${RONOTE_E2E_PROJECT:?RONOTE_E2E_PROJECT is not set}
  in_postgres() { sh "$(dirname "$0")/container.sh" compose -p "$project" exec -T postgres "$@"; }
  { in_postgres dropdb -U ronote --if-exists --force "$name" && in_postgres createdb -U ronote "$name"; } || {
    echo "e2e-db: cannot recreate $name in the Postgres of compose project $project; run make e2e-stack-up first" >&2
    exit 1
  }
fi

go tool goose -dir migrations postgres "$RONOTE_E2E_DATABASE_URL" up
