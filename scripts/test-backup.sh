#!/bin/sh
# Runs deploy/backup/backup.sh and restore-drill.sh against a fake podman,
# which logs every call and answers from variables the test sets. Secrets
# sit in this environment under the names a careless script might read;
# none may reach a podman argument or the scripts' output. Exits non-zero
# if any case fails.
set -u

scripts=$(cd "$(dirname "$0")" && pwd)
backup=$scripts/../deploy/backup
work=$(mktemp -d "${TMPDIR:-/tmp}/backup-test.XXXXXX")
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

secret=$(od -An -N8 -tx1 /dev/urandom | tr -d ' \n')
RONOTE_RESTIC_PASSWORD=$secret RESTIC_PASSWORD=$secret RONOTE_BACKUP_SECRET_KEY=$secret NTFY_TOPIC=$secret
export RONOTE_RESTIC_PASSWORD RESTIC_PASSWORD RONOTE_BACKUP_SECRET_KEY NTFY_TOPIC

# The fake. REPO is "none" until init; COUNT_FAILS makes the row count
# fail; ROWS is what psql counts in the drill; STATUS is migrate status's
# exit; RESTORE_DIR is where restic restore writes the backup.
bin=$work/bin
mkdir -p "$bin"
cat >"$bin/podman" <<'FAKE'
#!/bin/sh
printf '%s\n' "$*" >>"$LOG"
case $* in
  "container inspect"*ronote-postgres) echo localhost/postgres:test ;;
  "container inspect"*ronote-api) echo localhost/api:test ;;
  "exec ronote-postgres pg_dump"*) echo DUMP ;;
  *"pg_restore --data-only"*)
    [ -z "${COUNT_FAILS:-}" ] || { echo "pg_restore: error: input file is too short" >&2; exit 1; }
    printf 'public.users\t2\npublic.documents\t5\n' ;;
  *" cat config") [ "$(cat "$REPO")" = made ] || exit 10 ;;
  *" init") echo made >"$REPO" ;;
  *" restore latest"*)
    mkdir -p "$RESTORE_DIR/backup"
    echo DUMP >"$RESTORE_DIR/backup/ronote.dump"
    printf 'public.users\t2\npublic.documents\t5\n' >"$RESTORE_DIR/backup/counts.tsv" ;;
  *"psql"*"public.users") echo 2 ;;
  *"psql"*"public.documents") echo "${ROWS:-5}" ;;
  *" migrate status") echo "schema at version 19; this build's newest is 19"; exit "${STATUS:-0}" ;;
  *" search-check") echo "search: 3 hits in the newest chunk's library" ;;
esac
FAKE
chmod +x "$bin/podman"

etc=$work/etc
mkdir -p "$etc"
printf 'restic=localhost/restic:test\nrclone=localhost/rclone:test\n' >"$etc/backup.images"
printf 'RONOTE_BLOB_ENDPOINT=http://ronote-seaweedfs:8333\nRONOTE_BLOB_REGION=us-east-1\nRONOTE_BLOB_BUCKET=ronote\n' >"$etc/ronote.env"

LOG=$work/log
REPO=$work/repo
RESTORE_DIR=$work/drill
export LOG REPO RESTORE_DIR

# run [VAR=VALUE...] SCRIPT ARG...: runs it with the fake and those set,
# leaving its output in $out, its exit in $status and the podman calls in
# $log. (Assignments before a function call may outlast it, so they are
# arguments.)
run() {
  : >"$LOG"
  vars=
  while :; do
    case $1 in
      *=*) vars="$vars $1"; shift ;;
      *) break ;;
    esac
  done
  script=$1
  shift
  # shellcheck disable=SC2086 # one word per assignment
  out=$(env $vars PATH="$bin:$PATH" RONOTE_ETC="$etc" RONOTE_BACKUP_WORK="$RESTORE_DIR" sh "$backup/$script" "$@" 2>&1)
  status=$?
  log=$(cat "$LOG")
}

ran() {
  printf '%s\n' "$log" | grep -qF -- "$1"
}

# clean: no secret in the podman calls or the output.
clean() {
  case "$log$out" in *"$secret"*) return 1 ;; esac
}

# holds NAME COMMAND...: NAME passes when COMMAND succeeds.
holds() {
  name=$1
  shift
  if "$@"; then
    result "$name" ok
  else
    result "$name" "exit $status; said: $out; ran: $log"
  fi
}

failed_before() {
  [ "$status" -ne 0 ] && ! ran "$1"
}

cleaned_up() {
  ran 'rm -f -v ronote-drill-postgres' && ran 'network rm -f ronote-drill'
}

echo none >"$REPO"
run backup.sh db
if [ "$status" -ne 0 ]; then
  result 'the database backup succeeds' "exit $status: $out"
else
  result 'the database backup succeeds' ok
fi
holds 'a missing repository is made first' ran ' init'
holds 'the dump and its counts go in, tagged db' ran 'backup --tag db --host ronote --quiet /backup'
holds 'it keeps 7 daily, 4 weekly, 6 monthly' ran 'forget --tag db --keep-daily 7 --keep-weekly 4 --keep-monthly 6 --prune'
holds 'restic gets its password from podman' ran '--secret ronote-restic-password,type=env,target=RESTIC_PASSWORD'
holds 'no secret is in an argument or the output' clean

run COUNT_FAILS=1 backup.sh db
holds 'a dump that fails to count fails the backup, before restic' failed_before 'backup --tag db'

run backup.sh pdfs
if [ "$status" -ne 0 ] || ! ran 'sync blobs:ronote /mirror' || ! ran 'backup --tag pdfs'; then
  result 'the PDFs are mirrored, then backed up, tagged pdfs' "exit $status: $out; ran: $log"
else
  result 'the PDFs are mirrored, then backed up, tagged pdfs' ok
fi
holds 'no secret in the PDF backup' clean

run restore-drill.sh
case $status:$out in
  0:*"passed: 2 tables as the backup counted them"*) result 'the drill passes when every table matches' ok ;;
  *) result 'the drill passes when every table matches' "exit $status: $out" ;;
esac
holds 'the scratch database is removed' cleaned_up

run ROWS=4 restore-drill.sh
case $status:$out in
  0:*) result 'a table that differs fails the drill, by name' "passed: $out" ;;
  *"public.documents (4 rows, the backup counted 5)"*) result 'a table that differs fails the drill, by name' ok ;;
  *) result 'a table that differs fails the drill, by name' "exit $status: $out" ;;
esac
holds 'a failed drill still removes its database' cleaned_up

run STATUS=3 restore-drill.sh
holds 'a schema behind this build fails the drill' failed_before 'search-check'
holds 'no secret in the drill' clean

printf '%s passed, %s failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
