#!/usr/bin/env bash
# The restore drill: put a backup back and prove what came out is what went in.
#
#	scripts/restore_drill.sh (--from BACKUP_DIR | --url URL)
#		   [--files DIR] [--app-url URL] [--keep]
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
# --files is the installation's own byte store (the directory file.Local writes
# to). It is what makes the object half a comparison rather than a tautology:
# restoring the backup and re-reading that copy asks whether `cp` copies. Say it
# and the drill also asks whether the backup carried what the installation holds
# right now, in both directions. Leave it out and the byte half can only say the
# backup still reads like itself, which is what the closing line then says.
#
# --app-url is the *application's* connection — the unprivileged role, not the
# owner — and it defaults to PLATFORMKIT_TEST_DATABASE_URL, which is what `make
# restore-drill` already exports. The same read of the source and of the restore
# asks which tables that role can SELECT from, and the two sets have to agree.
# A restore the application cannot open is not a restore of this
# installation however faithfully its bytes came back. The dump carries the grants
# (scripts/backup.sh dumps them for exactly this check), so a restore that lost
# them arrives as a failed drill rather than a pass that locks the application out
# of every table.
#
# Each check is counted, because they fail apart from one another:
#
#   objects   three questions when --files names the store — does the backup name
#             exactly the objects the installation holds (both directions), do the
#             installation's current bytes still read as the manifest recorded
#             them, and did the restored copy come back digest for digest — and
#             only the last when it names nothing. A blob the backup dropped is
#             the failure this module's shape (rows in Postgres, bytes in a store)
#             makes possible, and no row check sees it: the rows say a file
#             exists while the store says it does not.
#   grants    the set of tables the application's own role can read, read the same
#             way on both sides and compared. This proves reachability, not
#             equality — a value read under row-level security needs a tenant,
#             which is the rows half's job as the owner. A table the installation
#             itself never granted to the role is shut on both sides: said, and not
#             counted as a failure.
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
#	1  a check failed — a byte, a missing or extra object, a table the application
#	   role cannot read, a row set, or a dump that differs from itself
#	2  the drill could not run: no tool, no connection, no backup, nothing to sample
#
# It creates and drops exactly one database, platformkit_drill_<pid>.
set -Eeuo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

url="${PLATFORMKIT_TEST_ADMIN_URL:-}"
app_url="${PLATFORMKIT_TEST_DATABASE_URL:-}"
from=""
files=""
keep=0

die() { echo "restore drill: $*" >&2; exit 2; }
note() { echo "restore drill: $*"; }
fail() { echo "restore drill: FAIL: $*" >&2; failed=1; }

while [ $# -gt 0 ]; do
	case "$1" in
	--from) [ $# -ge 2 ] || die "--from wants a backup directory"; from="$2"; shift 2 ;;
	--url) [ $# -ge 2 ] || die "--url wants a connection"; url="$2"; shift 2 ;;
	--files) [ $# -ge 2 ] || die "--files wants the installation's byte store"; files="${2%/}"; shift 2 ;;
	--app-url) [ $# -ge 2 ] || die "--app-url wants a connection"; app_url="$2"; shift 2 ;;
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
	# The backup the drill takes for itself is the backup the drill drills, so it has
	# to carry the half of the installation --files named. backup.sh copies blobs only
	# when it is told where they live; a drill that forwarded nothing spent its first
	# counted check refusing a backup of its own making, which fails every default run
	# on exactly the installations that hold a byte store and blames a backup nobody
	# asked for. `make restore-drill` names --files whenever the deployment has one.
	backup_args=(--url "$url" --out "$temporary")
	[ -z "$files" ] || backup_args+=(--files "$files")
	from="$("$root/scripts/backup.sh" "${backup_args[@]}" |
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
# The application's own connection is not optional: the question this drill owes is
# whether the installation can be put back into use, and only its role can answer
# that. `make restore-drill` exports it; a hand-run drill passes --app-url.
[ -n "$app_url" ] || die "no application connection: pass --app-url or set PLATFORMKIT_TEST_DATABASE_URL — a restore only its owner can open is not a restore"
app_scratch_url="$(with_database "$app_url" "$scratch")"
matched=0
checked=0
rows_failed=0
compared_the_installation=0

# --- objects -----------------------------------------------------------------
manifest_objects() { awk 'NF==2 && $1 ~ /^[0-9a-f]{64}$/ && $2 !~ /\.dump$/ {print $1"  "$2}' "$manifest"; }

if [ -d "$from/objects" ]; then
	# Restoring is a copy of what the backup carried, and every digest the manifest
	# took before anything moved is asked of the file that arrived. On its own that
	# half asks only whether `cp` copied; --files is what makes it a comparison.
	cp -a -- "$from/objects" "$work/objects"
	if [ -n "$files" ]; then
		[ -d "$files" ] || die "no object store directory at $files"
		compared_the_installation=1
		# Direction one, as one counted check: the two listings are the same set.
		# An object the installation holds and the backup does not name is a blob
		# this backup would have lost; one the manifest names and the store no
		# longer holds is the same sentence about the other side.
		checked=$((checked + 1))
		in_store="$(cd "$files" && find . -type f -print | sed 's#^\./##' | LC_ALL=C sort)"
		in_manifest="$(manifest_objects | awk '{print $2}' | LC_ALL=C sort)"
		if [ "$in_store" = "$in_manifest" ]; then
			matched=$((matched + 1))
		else
			lost="$(comm -23 <(printf '%s\n' "$in_store") <(printf '%s\n' "$in_manifest") | tr '\n' ' ')"
			extra="$(comm -13 <(printf '%s\n' "$in_store") <(printf '%s\n' "$in_manifest") | tr '\n' ' ')"
			fail "$files holds objects this backup does not carry [${lost:-none}] and the backup carries objects the store does not hold [${extra:-none}] — one backup is one instant, and either list says a restore from it would not reproduce this store"
		fi
		# Direction two: the bytes the backup digested are still the bytes there.
		# Same name, different content is the drift a name-only listing cannot see.
		checked=$((checked + 1))
		drifted=""
		while read -r want name; do
			here="$(sha256sum -- "$files/$name" 2>/dev/null | cut -d' ' -f1 || true)"
			[ "$here" = "$want" ] || drifted="$drifted $name(reads ${here:-nothing})"
		done < <(manifest_objects)
		if [ -z "$drifted" ]; then
			matched=$((matched + 1))
		else
			fail "the backup's digests no longer describe $files:${drifted} — the store moved after the manifest was taken, so this backup is not a copy of it"
		fi
	else
		note "objects: no --files, so the byte half compares the backup against the copy the drill restored and never opens the installation's store"
	fi
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
	done < <(manifest_objects)
elif [ -n "$files" ]; then
	# The installation has a byte store and this backup carried nothing for it. The
	# rows would come back naming files no restore can serve, and no other check
	# here would notice: the rows half compares rows with rows.
	compared_the_installation=1
	checked=$((checked + 1))
	count_live="$(find "$files" -type f | wc -l | tr -d ' ')"
	fail "$files holds $count_live objects and $from carries no byte store at all — a restore of this backup puts the rows back with nothing for them to point at"
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
# The dump carries the grants, so the restore applies them: scripts/backup.sh
# dumps privileges for exactly this reason. Roles are still not dumped — creating
# platformkit_app is the cluster owner's job — but a database that comes back with
# no privileges on its tables is a database nobody but its owner can open.
restore_script="$work/restore.sql"
pg_restore --no-owner -f "$restore_script" "$dump" ||
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

# --- grants ------------------------------------------------------------------
# The same tables, read as the role the installation actually runs as, on both
# sides. The owner connection answers nothing here: it holds every table by
# construction, so a restore that brought no grants back at all still reads
# perfectly to it. One bounded query per table — reachability, not values, because
# a value read under row-level security needs a tenant and the rows half above
# already compares those as the owner. Both sides are read rather than the restore
# alone: a table the installation itself never granted to the role is nobody's
# failed restore, while one readable in the source and shut in the copy is exactly
# one.
checked=$((checked + 1))
# readable_in <connection> — the tables the application's role can SELECT from,
# in the same sorted shape list_tables gives. A failed read says nothing about
# the restore on its own (a table the installation itself never granted to the
# role is nobody's drill failure); the same read of both sides says a great deal.
readable_in() {
	local conn="$1" table err
	while IFS= read -r table; do
		[ -n "$table" ] || continue
		if err="$(psql "$conn" -qtAX -v ON_ERROR_STOP=1 -c "SELECT 1 FROM \"public\".$table LIMIT 1" 2>&1 >/dev/null)"; then
			printf '%s\n' "$table"
		else
			printf '%s: %s\n' "$table" "$(printf '%s' "$err" | head -1)" >&2
		fi
	done <<< "$(list_tables "$conn")" | LC_ALL=C sort
}
app_source_url="$(with_database "$app_url" "$source_db")"
app_source_err="$work/app-source.err"
app_scratch_err="$work/app-scratch.err"
readable_source="$(readable_in "$app_source_url" 2>"$app_source_err")"
readable_scratch="$(readable_in "$app_scratch_url" 2>"$app_scratch_err")"
shut="$(comm -23 <(printf '%s\n' "$tables_restored") <(printf '%s\n' "$readable_scratch") | tr '\n' ' ')"
app_err="$(head -1 "$app_scratch_err" 2>/dev/null || true)"
if [ -n "$tables_restored" ] && [ -z "$readable_scratch" ]; then
	# The two sides could agree by both reading nothing, and that is the one shape
	# this check must never pass on: an app connection that cannot connect at all,
	# or a restore with no grants left on it, both look like this.
	fail "the application's own role reads no table of $scratch at all (${app_err:-no read error named}) — a restored database nothing but its owner can query is not a restored installation"
elif [ "$readable_source" = "$readable_scratch" ]; then
	matched=$((matched + 1))
	if [ -n "$shut" ]; then
		note "$shut the application's role reads in neither database"
	fi
else
	lost="$(comm -23 <(printf '%s\n' "$readable_source") <(printf '%s\n' "$readable_scratch") | tr '\n' ' ')"
	gained="$(comm -13 <(printf '%s\n' "$readable_source") <(printf '%s\n' "$readable_scratch") | tr '\n' ' ')"
	why=""
	if [ -n "$app_err" ]; then
		why=" — the first read that failed in $scratch: $app_err"
	fi
	fail "the application's own role reads a different set of tables after the restore: [${lost:-none}] it can read in $source_db and not in $scratch, [${gained:-none}] it can read in $scratch and not in $source_db — the first names a restore that came back shut against the installation it is supposed to serve, the second one that hands the role more than the installation does$why"
fi

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
if [ "$compared_the_installation" -ne 0 ]; then
	note "$from restored byte-identical across $matched checks"
else
	note "$from restored to the same bytes and rows across $matched checks — the byte half compared the backup with the copy the drill restored and never opened the installation's store: pass --files to compare that too"
fi
