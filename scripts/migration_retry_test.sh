#!/usr/bin/env bash
# A migration that failed leaves nothing behind, and the same run again applies it once.
#
# The runner applies a transactional file and its ledger row in one transaction
# (kit/db/migrate.go), so the retry promise has a precise shape: a file that failed
# mid-way is *absent* — no history row, none of its own statements — while every file
# that committed before it stays committed, and the next run treats the failed file as
# pending and applies it once. This measures that on a real database with a real
# pending file, and not on an empty schema:
#
#   1. one disposable database, migrated by a binary built from this tree with the
#      release's last root migration broken by one statement that cannot execute;
#   2. the refusal: the broken file has no ledger row, the table it exists to create
#      does not exist, and the files that committed before it are on the ledger —
#      a half-applied file and a wiped history are both worse than a refusal;
#   3. the same database, migrated by a binary built from the same tree with the file
#      restored to its own bytes: it applies, once, with no earlier row rewritten.
#
# Nothing here touches a stored checksum or an applied row: the broken file is broken in
# a copy of this tree exported by `git archive`, never in the checkout, and the ledger is
# read, never written. Needs PLATFORMKIT_TEST_ADMIN_URL and psql; without them it says it
# could not run rather than passing.
set -uo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
work=$(mktemp -d)
db="platformkit_retry_$$"
fails=0

cleanup() {
	if [ -n "${PLATFORMKIT_TEST_ADMIN_URL:-}" ]; then
		psql "$PLATFORMKIT_TEST_ADMIN_URL" -q -c "DROP DATABASE IF EXISTS $db" >/dev/null 2>&1 ||
			echo "retry: LEFT BEHIND $db: it could not be dropped" >&2
	fi
	rm -rf "$work"
}
trap cleanup EXIT

admin_url="${PLATFORMKIT_TEST_ADMIN_URL:-}"
app_url="${PLATFORMKIT_TEST_DATABASE_URL:-}"
if [ -z "$admin_url" ] || ! command -v psql >/dev/null || ! command -v go >/dev/null; then
	echo "retry: refused — PLATFORMKIT_TEST_ADMIN_URL, psql and go are what the retry is measured with, and one of them is missing; a retry nobody ran is not a pass, so this exits 2 rather than skipping"
	exit 2
fi

with_database() {
	local rest="${1#*://}" authority tail="" query=""
	if [[ "$rest" == */* ]]; then
		authority="${rest%%/*}"
		tail="${rest#*/}"
	else
		authority="$rest"
	fi
	[[ "$tail" == *\?* ]] && query="?${tail#*\?}"
	printf '%s://%s/%s%s' "${1%%://*}" "$authority" "$2" "$query"
}
db_admin_url="$(with_database "$admin_url" "$db")"
db_app_url="$(with_database "${app_url:-$admin_url}" "$db")"

check() { # <sentence> <expected> <actual>
	if [ "$2" = "$3" ]; then
		echo "retry: ok — $1: $3"
		return
	fi
	echo "retry: FAIL — $1: expected $2, got $3" >&2
	fails=$((fails + 1))
}

# The file under test: the highest-numbered root migration of this tree, which every
# supported installation holds as pending, and which is transactional rather than an
# autocommit or a drain — the mode whose whole contract is that it is all one transaction.
file="migrations/000072_command_effect.up.sql"
[ -r "$root/$file" ] || { echo "retry: FAIL — $file is not in this tree to break" >&2; exit 1; }
grep -q 'CREATE TABLE platformkit_command_effect' "$root/$file" ||
	{ echo "retry: FAIL — $file creates no table to look for, so the case would assert nothing" >&2; exit 1; }

tree="$work/tree"
mkdir -p "$tree"
git -C "$root" archive HEAD | tar -x -C "$tree" || { echo "retry: FAIL — this tree could not be exported" >&2; exit 1; }
cp "$root/$file" "$work/original.sql"

write_config() { # <file> <app url> — the same declaration the rehearsal hands its candidate, so this
# case refuses and applies for the migration's own reasons and not for a missing key.
	cat >"$1" <<YAML
server:
  addr: ":0"
  public_host: "retry.localhost"
database:
  url: "$2"
  migrate_url: "$db_admin_url"
nats:
  url: "nats://127.0.0.1:4222"
log:
  level: "info"
YAML
	cat "$root/scripts/testdata/rehearse/candidate.yaml" >>"$1"
}
build() { # <output> — the exported tree carries no .git, so Go's stamp step is asked to skip
	(cd "$tree" && go build -buildvcs=false -o "$1" ./apps/platformkit) ||
		{ echo "retry: FAIL — the exported tree does not build" >&2; exit 1; }
}
# This file runs without `set -e`, because its first act is to ask for a run that fails:
# a helper that toggled errexit would end the case at the refusal it is looking for.
migrate() { # <binary> <log>: 0 when the run said it applied everything pending
	"$1" migrate --config "$work/run.yaml" >"$2" 2>&1
}

psql_admin() { psql "$db_admin_url" -v ON_ERROR_STOP=1 -qtA -c "$1"; }
broken_row() { psql_admin "SELECT count(*) FROM schema_migrations WHERE name = '000072_command_effect.up.sql'"; }
built_table() { psql_admin "SELECT count(*) FROM pg_class WHERE relname = 'platformkit_command_effect'"; }
applied_count() { psql_admin "SELECT count(*) FROM schema_migrations"; }

psql "$admin_url" -q -c "CREATE DATABASE $db" || exit 1
write_config "$work/run.yaml" "$db_app_url"

# 1. The run that fails, halfway through the release's own last root file.
printf '\nSELECT 1 / 0;\n' >>"$tree/$file"
build "$work/broken"
migrate "$work/broken" "$work/broken.log"
if [ $? -eq 0 ]; then
	echo "retry: FAIL — a migration whose statement cannot execute reported success" >&2
	fails=$((fails + 1))
else
	echo "retry: ok — the run that could not execute the statement refused"
fi
grep -qi 'error' "$work/broken.log" ||
	echo "retry: note — the refusing run printed no error line; its exit is still a refusal" >&2

# 2. What the failed run left: nothing of its own, and everything of anybody else's.
check "the failed file has no ledger row" "0" "$(broken_row)"
check "the table that file exists to create was not left standing" "0" "$(built_table)"
applied_before=$(applied_count)
if [ "${applied_before:-0}" -gt 20 ]; then
	echo "retry: ok — the $applied_before files that committed before it stayed committed"
else
	echo "retry: FAIL — only $applied_before file(s) on the ledger after a run that reached the last root migration" >&2
	fails=$((fails + 1))
fi
# The ledger the failed run left, row by row, is the ledger the retry must still hold:
# a retry that rewrote an applied row or restamped one is the immutability refusal, and
# the only line the second run may add is the retried file's own.
ledger_rows() { psql_admin "SELECT owner || '/' || version || '|' || name || '|' || checksum FROM schema_migrations ORDER BY 1" | sort; }
ledger_rows >"$work/ledger-before.txt"

# The retry: the file's own bytes, a binary rebuilt from them, the same database.
cp "$work/original.sql" "$tree/$file"
build "$work/fixed"
if ! migrate "$work/fixed" "$work/fixed.log"; then
	tail -20 "$work/fixed.log" >&2
	echo "retry: FAIL — the same database refused the file once its own bytes were back" >&2
	fails=$((fails + 1))
else
	echo "retry: ok — the retry run applied what the failed run could not"
fi
check "the retried file is on the ledger exactly once" "1" "$(broken_row)"
check "the table it creates exists" "1" "$(built_table)"
ledger_rows >"$work/ledger-after.txt"
rewritten=$(comm -23 "$work/ledger-before.txt" "$work/ledger-after.txt" | head -3)
check "no applied row was rewritten or lost by the retry" "" "$rewritten"
# A retried run applies the file that failed *and* every file still behind it — the run
# stopped where it stopped — so the claim is not a row count. It is that no row arrived
# twice, and that a third run of the same binary finds nothing left to do.
check "no (owner, version) arrived twice" "0" \
	"$(psql_admin "SELECT count(*) FROM (SELECT owner, version FROM schema_migrations GROUP BY owner, version HAVING count(*) > 1) AS doubled")"
if migrate "$work/fixed" "$work/third.log"; then
	echo "retry: ok — the same run again ended without a refusal"
else
	tail -10 "$work/third.log" >&2
	echo "retry: FAIL — the run after the retry did not converge" >&2
	fails=$((fails + 1))
fi
ledger_rows >"$work/ledger-third.txt"
check "and it applied nothing: the ledger it left is the ledger it read" "" \
	"$(comm -3 "$work/ledger-after.txt" "$work/ledger-third.txt" | head -3)"

if [ "$fails" -gt 0 ]; then
	echo "retry: failed: $fails check(s)" >&2
	exit 1
fi
echo "retry: ok — a failed migration left no ledger row and no object, kept what had committed, and applied once on the run that followed"
