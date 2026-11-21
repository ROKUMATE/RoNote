# Each table's rows in a dump's data, as pg_restore --data-only writes it:
# each COPY block holds one line a row (docs/adr/0015-operations.md).
/^COPY [^ ]+ .* FROM stdin;$/ { table = $2; rows = 0; copying = 1; next }
copying && $0 == "\\." { print table "\t" rows; copying = 0; next }
copying { rows++ }
