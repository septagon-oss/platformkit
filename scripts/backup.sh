#!/usr/bin/env bash
# The backup step: one file per database and one manifest of the bytes.
#
#	scripts/backup.sh [--url URL] [--files DIR] [--out DIR] [--prefix NAME]
#
# What an installation has to be able to put back is two things that live in two
# places and age at different rates: the rows, which name every file a tenant
# holds, and the blobs, which the rows only point at by key. This step writes both
# in one go and names the digest of each, because the question a restore drill asks
# is not "did the restore run" but "is what came back the same thing", and a
# question about sameness needs a number taken *before* the restore, not after it.
#
# The dump is custom format (`pg_dump -Fc`): a directory a later `pg_restore` reads
# selectively, compressed, and restorable in parallel. Roles are not dumped — a
# cluster is not this database's role directory, and `platformkit_app` is created
# from apps/platformkit/postgres-init.sql by whoever owns the cluster (the same
# promise scripts/rehearse_migrations.sh makes about a rehearsal cluster).
#
# --files is the object store's directory for a deployment that runs on disk
# (`file.Local`); a deployment on an object store has its own replication and
# passes nothing, and the manifest then says the bytes were not part of this backup
# rather than being silent about a whole half of the installation.
#
# Exit codes: 0 wrote a complete backup; 2 could not start one (no tool, no
# connection, an unreadable directory). It never touches the source.
set -Eeuo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

url="${PLATFORMKIT_TEST_ADMIN_URL:-}"
files=""
out="backups"
prefix="platformkit"

die() { echo "backup: $*" >&2; exit 2; }
note() { echo "backup: $*"; }

while [ $# -gt 0 ]; do
	case "$1" in
	--url) [ $# -ge 2 ] || die "--url wants a value"; url="$2"; shift 2 ;;
	--files) [ $# -ge 2 ] || die "--files wants a directory"; files="$2"; shift 2 ;;
	--out) [ $# -ge 2 ] || die "--out wants a directory"; out="$2"; shift 2 ;;
	--prefix) [ $# -ge 2 ] || die "--prefix wants a value"; prefix="$2"; shift 2 ;;
	*) die "unknown argument: $1 (see the header)" ;;
	esac
done

[ -n "$url" ] || die "no connection: pass --url or set PLATFORMKIT_TEST_ADMIN_URL"
for tool in pg_dump psql sha256sum; do
	command -v "$tool" >/dev/null || die "$tool is not installed"
done

# The database name comes out of the connection string, because the stamp in the
# filename has to say which database a file is for and there is no second source
# for that fact worth keeping in sync.
dbname="$(psql "$url" -qtAX -c 'SELECT current_database()')" || die "cannot open $url"
[ -n "$dbname" ] || die "$url names no database"

stamped="$out/${prefix}-${dbname}-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$stamped"

dump="$stamped/${dbname}.dump"
pg_dump --no-owner --no-privileges --no-tablespaces -Fc -f "$dump" "$url"

{
	echo "# platformkit backup of database $dbname, $(date -u +%FT%TZ)"
	printf '%s  %s\n' "$(sha256sum "$dump" | cut -d' ' -f1)" "${dbname}.dump"
} > "$stamped/manifest.sha256"

if [ -n "$files" ]; then
	[ -d "$files" ] || die "no object store directory at $files (a deployment on an object store passes no --files)"
	cp -a -- "$files" "$stamped/objects"
	(
		cd "$stamped/objects" && find . -type f -print0 | sort -z |
			xargs -0 sha256sum | sed 's#  \./#  #'
	) >> "$stamped/manifest.sha256"
	blobs="$(find "$stamped/objects" -type f | wc -l | tr -d ' ')"
	note "$dbname: $dump, $stamped/objects ($blobs objects), $stamped/manifest.sha256"
else
	note "objects: not part of this backup (no --files); the dump is the whole of it"
	note "$dbname: $dump, $stamped/manifest.sha256"
fi
