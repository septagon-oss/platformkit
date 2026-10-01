#!/usr/bin/env bash
# The restore drill's own default path, on a machine that has a byte store.
#
#	scripts/restore_drill_store_test.sh
#
# `make restore-drill` runs the drill with no --from, so the drill takes the backup
# it drills (scripts/restore_drill.sh's --from block). `make restore-drill` also
# passes --files whenever the deployment's byte store directory is there
# (PLATFORMKIT_FILES_DIR), because that half of the installation is the half this
# task is about. Those two facts have to agree: a drill told where the installation's
# objects live has to drill a backup that carries them, or the very first check it
# makes — "this backup carried no byte store for an installation that has one" — is a
# sentence about the drill's own arguments, not about the installation.
#
# This runs the drill end to end against a database the test fakes: psql, pg_dump and
# pg_restore answer on PATH, so no cluster is touched and no database is created, and
# the only question asked is the one the drill answers from its own reasoning — did
# the backup it took carry the objects it was told about, yes or no.
#
# Exit codes: 0 a clean default run drills green; 1 the drill refused its own default.
set -Eeuo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

fail=0
note() { echo "restore drill store test: $*"; }

bin="$work/bin"
store="$work/files"
backup="$work/backup"
mkdir -p "$bin" "$store" "$backup"

# A three-object byte store, named the way file.Local names them: a tenant's id, then
# the key the row carries.
for pair in "11111111-1111-1111-1111-111111111111/blob alpha" \
            "22222222-2222-2222-2222-222222222222/blob beta" \
            "33333333-3333-3333-3333-333333333333/blob gamma"; do
	set -- $pair
	mkdir -p "$store/$(dirname "$1")"
	printf '%s' "$2" > "$store/$1"
done

# psql answers the handful of questions the drill and the backup step ask. Every one
# of them is a question about a database this test has decided in advance; the drill
# is not being asked to prove anything about the answers.
cat > "$bin/psql" <<'PSQL'
#!/usr/bin/env bash
query=""
while [ $# -gt 0 ]; do
	case "$1" in
	-c) query="${2:-}"; shift 2 ;;
	*) shift ;;
	esac
done
case "$query" in
*"current_database()") echo drillsrc ;;
*"CREATE DATABASE"*) : ;;
*"DROP DATABASE"*) : ;;
*"pg_tables"*) printf '"invoice"\n' ;;
*"count(*)"*) echo '3|3d60f8d7e73e3f7f5b0edbe3b6c0bd27' ;;
*"SELECT 1 FROM"*) echo 1 ;;
*) : ;;
esac
exit 0
PSQL
# pg_dump writes a stand-in artefact: bytes enough to digest, and identical every
# time, which is what the drill's double-dump check needs to be able to pass.
cat > "$bin/pg_dump" <<'DUMP'
#!/usr/bin/env bash
out=""
prev=""
for a in "$@"; do
	[ "$prev" = "-f" ] && out="$a"
	prev="$a"
done
[ -n "$out" ] || exit 0
if [ ! -e "$out" ]; then printf 'platformkit stand-in dump\n' > "$out"; fi
exit 0
DUMP
cat > "$bin/pg_restore" <<'RESTORE'
#!/usr/bin/env bash
out=""
prev=""
for a in "$@"; do
	[ "$prev" = "-f" ] && out="$a"
	prev="$a"
done
[ -n "$out" ] || exit 0
: > "$out"
exit 0
RESTORE
chmod +x "$bin/psql" "$bin/pg_dump" "$bin/pg_restore"

url='postgres://postgres@localhost:1/drillsrc?sslmode=disable'
app_url='postgres://platformkit_app@localhost:1/drillsrc?sslmode=disable'

# The default `make restore-drill` invocation on an installation with a byte store:
# --files named, no --from, so the drill makes its own backup and drills that. The
# backup directory is the drill's own temporary one and is gone by the time it exits,
# so what is asserted is the sentence the drill writes at the end of a run it believes:
# exit 0 and "restored byte-identical". A drill that reaches exit 1 here is refusing a
# backup it took for itself, which is its own argument, not the installation's.
set +e
drill="$(PATH="$bin:$PATH" "$root/scripts/restore_drill.sh" --url "$url" --app-url "$app_url" \
	--files "$store" 2>&1)"
code=$?
set -e
printf '%s\n' "$drill" | sed 's/^/  | /'

if [ "$code" -ne 0 ]; then
	note "FAIL: a clean installation with a byte store must drill green on the default path, exit $code"
	fail=1
elif printf '%s' "$drill" | grep -q 'restored byte-identical'; then
	note "the default path drills green and says byte-identical"
else
	note "FAIL: a run named --files ended 0 without saying the bytes were compared with the installation's store"
	fail=1
fi

[ "$fail" -eq 0 ] || exit 1
note "ok"
