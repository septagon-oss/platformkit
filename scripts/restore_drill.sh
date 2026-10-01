#!/usr/bin/env bash
# The restore drill: put a backup back and prove what came out is what went in.
#
#	scripts/restore_drill.sh (--from BACKUP_DIR | --url URL) [--keep]
#
# A backup that has never been restored is a hope, not a copy. ADR 0011 makes the
# rehearsal a release step for migrations; this is its other half, the one about
# the storage task's promise — that a restore reproduces the installation byte
# identically. The only way to answer that question is to restore somewhere it does
# not matter and compare, which is what this does, on the artefact an operator would
# actually put back rather than on the running database.
#
# --from is a directory scripts/backup.sh wrote. --url (or the environment's
# PLATFORMKIT_TEST_ADMIN_URL) takes one first, so the drill always drills the backup
# and never the live server.
#
# Three checks, each counted, because they fail apart from one another:
#
#   objects   every file the manifest names is re-read where the byte store was
#             restored to and its digest compared with the one taken when the
#             backup ran. This is the byte-identical half: a blob that comes back
#             different is exactly the failure this module's shape (rows in
#             Postgres, bytes in a store) makes possible, and no row check sees it.
#   rows      every public table is read in both databases as one value — its row
#             count and an md5 over the rows' text in a fixed order — and the two
#             values compared. This is the half a byte check cannot see: the rows
#             that name the files, the holds, the erasure proofs. The first counted
#             check of this half is the set of tables itself, named in both
#             directions: reading table by table without it turned "the restore does
#             not have that table" into a SELECT that errors, which the exit codes
#             call a drill that could not run (2) rather than a restore that failed (1).
#   dump      the restored database is dumped twice and the dumps compared byte for
#             byte, so the artefact is a complete rendering of the restore rather
#             than of whatever the server had loaded when the first one ran.
#
# A drill can be contended. If the source takes writes while it runs, the rows check
# fails, and nothing here can tell "the restore lost a row" from "the source gained
# one": so a failed rows check says to rerun against a quiet database rather than
# counting the run as anything. A failed object check needs no such caveat — a byte
# that came back different is a finding whatever else is happening.
#
# Exit codes, so a pipeline can tell them apart:
#
#	0  every check passed, and the ratio is printed
#	1  a check failed — a byte, a row set, or a dump that differs from itself
#	2  the drill could not run: no tool, no connection, no backup, nothing to sample
#
# It creates and drops exactly one database, platformkit_drill_<pid>.
set -Eeuo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

url="${PLATFORMKIT_TEST_ADMIN_URL:-}"
from=""
keep=0

die() { echo "restore drill: $*" >&2; exit 2; }
note() { echo "restore drill: $*"; }
fail() { echo "restore drill: FAIL: $*" >&2; failed=1; }

while [ $# -gt 0 ]; do
	case "$1" in
	--from) [ $# -ge 2 ] || die "--from wants a backup directory"; from="$2"; shift 2 ;;
	--url) [ $# -ge 2 ] || die "--url wants a connection"; url="$2"; shift 2 ;;
	--keep) keep=1; shift ;;
	*) die "unknown argument: $1 (see the header)" ;;
	esac
done

for tool in psql pg_dump pg_restore sha256sum; do
	command -v "$tool" >/dev/null || die "$tool is not installed"
done
[ -n "$url" ] || die "no connection: pass --url or set PLATFORMKIT_TEST_ADMIN_URL"

work="$(mktemp -d)"
temporary="$work/backup"
scratch="platformkit_drill_$$"
cleanup() {
	if [ "$keep" -eq 0 ]; then
		psql "$url" -q -c "DROP DATABASE IF EXISTS $scratch" >/dev/null 2>&1 || true
	fi
	rm -rf "$work"
}
trap cleanup EXIT
failed=0

# One database inside the environment's own URI, everything else as written: the
# authority and the parameters stay whatever the operator's connection string says,
# and only the database name is replaced.
with_database() {
	local rest="${1#*://}" authority tail="" query=""
	if [[ "$rest" == */* ]]; then authority="${rest%%/*}"; tail="${rest#*/}"; else authority="$rest"; fi
	[[ "$tail" == *\?* ]] && query="?${tail#*\?}"
	printf '%s://%s/%s%s' "${1%%://*}" "$authority" "$2" "$query"
}

if [ -z "$from" ]; then
	from="$("$root/scripts/backup.sh" --url "$url" --out "$temporary" |
		sed -n 's#^backup: [^ ]*: \(.*\)/[^/]*\.dump,.*#\1#p' | tail -1)"
	[ -d "$from" ] || die "the backup this drill would have run wrote no directory"
fi
[ -d "$from" ] || die "no backup directory at $from"
dump="$(find "$from" -maxdepth 1 -name '*.dump' -print -quit)"
manifest="$from/manifest.sha256"
[ -n "$dump" ] || die "no .dump in $from"
[ -r "$manifest" ] || die "no manifest in $from: a backup that recorded no digests cannot be checked"

source_db="$(psql "$url" -qtAX -c 'SELECT current_database()')" || die "cannot open $url"
scratch_url="$(with_database "$url" "$scratch")"
matched=0
checked=0
rows_failed=0

# --- objects -----------------------------------------------------------------
# The byte store's restore is a copy of what the backup carried, and every digest
# the manifest took before anything moved is asked of the file that arrived.
if [ -d "$from/objects" ]; then
	cp -a -- "$from/objects" "$work/objects"
	while read -r want name; do
		case "${name:-}" in
		'' | \#*) continue ;;
		esac
		checked=$((checked + 1))
		have="$(sha256sum -- "$work/objects/$name" 2>/dev/null | cut -d' ' -f1 || true)"
		if [ "$have" = "$want" ]; then
			matched=$((matched + 1))
		else
			fail "object $name came back ${have:-with nothing at it}, the manifest recorded $want"
		fi
	done < <(awk 'NF==2 && $1 ~ /^[0-9a-f]{64}$/ && $2 !~ /\.dump$/ {print $1"  "$2}' "$manifest")
else
	note "objects: this backup carries no byte store, so the byte half counts nothing"
fi

# --- rows --------------------------------------------------------------------
psql "$url" -q -c "CREATE DATABASE $scratch" >/dev/null || die "cannot create $scratch"
# The same restoration the rehearsal step settled on, for the same measured reason:
# a client newer than the server adds its own session settings to the restore —
# pg_restore 18 emits `SET transaction_timeout = 0`, which PostgreSQL 16 refuses as
# an unrecognised parameter — and a drill that dies on its own tooling is a step that
# gets switched off. The line is dropped and said out loud; every other statement
# error still stops the drill, because ON_ERROR_STOP is on.
restore_script="$work/restore.sql"
pg_restore --no-owner --no-privileges -f "$restore_script" "$dump" ||
	die "pg_restore could not render $dump as a script"
if grep -q '^SET transaction_timeout = 0;$' "$restore_script"; then
	grep -v '^SET transaction_timeout = 0;$' "$restore_script" > "$restore_script.filtered"
	mv "$restore_script.filtered" "$restore_script"
	note "dropped the restoring client's own SET transaction_timeout: the server does not know that setting"
fi
psql "$scratch_url" -v ON_ERROR_STOP=1 -q -f "$restore_script" ||
	die "restoring $dump into $scratch left an error"

# Each table read as one value, in an order that does not depend on the heap: a
# sequential scan returns rows in whatever order they were written, so the
# aggregate sorts on the row's own text and the two databases have to agree.
digest() { # database name
	psql "$url" -qtAX -v ON_ERROR_STOP=1 -c \
		"SELECT count(*), coalesce(md5(string_agg(t::text, E'\n' ORDER BY t::text)),'') FROM \"public\".$1 t"
}
list_tables() { # connection
	psql "$1" -qtAX -c "
		SELECT quote_ident(tablename) FROM pg_tables
		 WHERE schemaname = 'public' AND tablename NOT LIKE 'pg\_%'" | LC_ALL=C sort
}
tables="$(list_tables "$url")" || die "cannot list the tables of $source_db"
tables_restored="$(list_tables "$scratch_url")" || die "cannot list the tables of $scratch"

# The two sets compared before any table is read, because a restore that lost a whole
# table has to arrive as a failed restore and not as a drill that could not run. Read
# table by table alone, a missing one is a SELECT that errors and `die` calls it exit 2
# — the code the header reserves for "no tool, no connection, no backup" — which is the
# one failure an operator most needs told apart from a bad cluster, and a pipeline that
# treats exit 2 as "retry the infrastructure" retries forever on a backup that is
# missing a table. The set is therefore its own counted check, and only the tables both
# sides have are then read row by row: what is missing has been said, and a digest of a
# table that is not there answers nothing.
checked=$((checked + 1))
if [ "$tables" = "$tables_restored" ]; then
	matched=$((matched + 1))
else
	# The two directions named apart, because the header's whole contract is that a
	# byte that came back different and a row set that moved are different sentences.
	# What the drill cannot tell — for the rows or the table set — is whether the table
	# was lost by the restore or moved in the source after the backup was taken; what it
	# can say is that the restore is not a copy of this database, which is a failed
	# drill (exit 1) and not a tool that broke (exit 2, "could not run").
	lost="$(comm -23 <(printf '%s\n' "$tables") <(printf '%s\n' "$tables_restored") | tr '\n' ' ')"
	gained="$(comm -13 <(printf '%s\n' "$tables") <(printf '%s\n' "$tables_restored") | tr '\n' ' ')"
	fail "$scratch holds a different set of tables from $source_db: [${lost:-none}] named by the source and not restored, [${gained:-none}] restored and not named by the source — one backup is one instant, so either list says the restore is not a copy of this database, whether the table was lost coming back or moved in the source after the backup was taken"
fi
both="$(comm -12 <(printf '%s\n' "$tables") <(printf '%s\n' "$tables_restored"))"
while IFS= read -r table; do
	[ -n "$table" ] || continue
	checked=$((checked + 1))
	from_source="$(digest "$table")" || die "cannot read $table in $source_db"
	from_scratch="$(psql "$scratch_url" -qtAX -v ON_ERROR_STOP=1 -c \
		"SELECT count(*), coalesce(md5(string_agg(t::text, E'\n' ORDER BY t::text)),'') FROM \"public\".$table t")" ||
		die "cannot read $table in $scratch"
	if [ "$from_source" = "$from_scratch" ]; then
		matched=$((matched + 1))
	else
		fail "$table reads [$from_source] in $source_db and [$from_scratch] after the restore"
		rows_failed=1
	fi
done <<< "$both"

# --- dump --------------------------------------------------------------------
# Both dumps drop the restoring client's own \restrict / \unrestrict lines: pg_dump
# 17.5+ mints a random token around a dump that could carry passwords, so two dumps
# of one quiet database differ at byte 46 by design. Comparing them with those lines
# in place would make this check fail every time, and comparing the files with them
# removed compares everything the dump actually says about the database.
dump_a="$work/a.sql"
dump_b="$work/b.sql"
dump_clean() { grep -vE '^\\(un)?restrict ' "$1" > "$1.clean"; }
pg_dump --no-owner --no-privileges --no-tablespaces -Fp -f "$dump_a" "$scratch_url" &&
	pg_dump --no-owner --no-privileges --no-tablespaces -Fp -f "$dump_b" "$scratch_url" ||
	die "pg_dump refused the restored database"
dump_clean "$dump_a" && dump_clean "$dump_b" || die "could not read back the two dumps"
checked=$((checked + 1))
if cmp -s "$dump_a.clean" "$dump_b.clean"; then
	matched=$((matched + 1))
else
	fail "the restored database dumps twice to different bytes: $(cmp "$dump_a.clean" "$dump_b.clean" 2>&1 | head -1)"
fi

[ "$checked" -gt 0 ] || die "the drill sampled nothing: no objects, no tables and no dump is not a pass"
note "restore_drill_pass_ratio=$matched/$checked"
if [ "$failed" -ne 0 ]; then
	if [ "$rows_failed" -ne 0 ]; then
		note "a failed rows check can also mean the source was written while the drill ran; rerun against a quiet database"
	fi
	exit 1
fi
note "$from restored byte-identical across $matched checks"
