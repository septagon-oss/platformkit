#!/usr/bin/env bash
# An applied migration whose bytes changed is refused, and nothing it touched survives.
#
# The runner compares a migration whole, so the comment-only edit that made a rehearsal
# refuse an already-applied copy is the failure this case reproduces on a real database:
# migrate a fresh database with this tree's own binary, then run a second binary built from
# the same tree with one comment line appended to the already-applied audit/000048. The
# mutant must refuse, and it must refuse without leaving a mark — the ledger row keeps the
# checksum it had, no row is added, and the seeded fact the first run recorded is the fact
# still there. The mutant earns no success and no new baseline: the check is against a
# snapshot taken before the mutant ever connected.
#
# Needs PLATFORMKIT_TEST_ADMIN_URL and PLATFORMKIT_TEST_DATABASE_URL, the two roles
# `make up` starts; with neither it says it could not run rather than passing.
set -uo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
work=$(mktemp -d)
db="platformkit_applied_bytes_$$"
pid=""
fails=0

cleanup() {
	if [ -n "$pid" ]; then
		kill "$pid" 2>/dev/null || true
		wait "$pid" 2>/dev/null || true
	fi
	if [ -n "${PLATFORMKIT_TEST_ADMIN_URL:-}" ]; then
		psql "$PLATFORMKIT_TEST_ADMIN_URL" -q -c "DROP DATABASE IF EXISTS $db" >/dev/null 2>&1 ||
			echo "rehearse: LEFT BEHIND $db: it could not be dropped" >&2
	fi
	rm -rf "$work"
}
trap cleanup EXIT

admin_url="${PLATFORMKIT_TEST_ADMIN_URL:-}"
app_url="${PLATFORMKIT_TEST_DATABASE_URL:-}"
if [ -z "$admin_url" ] || [ -z "$app_url" ] || ! command -v psql >/dev/null; then
	echo "applied-bytes: refused — PLATFORMKIT_TEST_ADMIN_URL, PLATFORMKIT_TEST_DATABASE_URL and psql are what the refusal is measured with, and one of them is missing; a refusal nobody ran is not a pass, so this exits 2 rather than skipping"
	exit 2
fi

psql_admin() { psql "$admin_url" -v ON_ERROR_STOP=1 -qtA -c "$1"; }

# One database name inside the URI the environment wrote, everything else — host, port,
# credentials, options — as it stands. Both halves of the configuration have to name this
# case's own database: `migrate` opens the owner connection it is handed and no other, and
# a configuration left pointing at the cluster's own database would migrate the suite's.
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
db_app_url="$(with_database "$app_url" "$db")"

write_config() { # $1 = file
	cat >"$1" <<YAML
server:
  addr: ":0"
  public_host: "rehearse.localhost"
database:
  url: "$db_app_url"
  migrate_url: "$db_admin_url"
nats:
  url: "nats://127.0.0.1:4222"
log:
  level: "info"
app:
  hosts:
    - "rehearse.localhost"
webhook:
  secret_key: "0123456789abcdef0123456789abcdef"
YAML
}

fail() {
	echo "FAIL  $*"
	fails=$((fails + 1))
}

psql_admin "CREATE DATABASE $db" >/dev/null || { echo "FAIL  no database to refuse in"; exit 1; }

# The candidate this tree builds, applied to a database of its own.
go build -o "$work/platformkit" ./apps/platformkit || { echo "FAIL  this tree does not build"; exit 1; }
write_config "$work/run.yaml"
if ! "$work/platformkit" migrate --config "$work/run.yaml" >"$work/clean.log" 2>&1; then
	tail -5 "$work/clean.log" >&2
	echo "FAIL  this tree's own migrate refused a fresh database"
	exit 1
fi

# What that run left behind, read before anything else touches the database: the whole
# ledger as one line per row, and the checksum the applied file was recorded under.
psql "$db_admin_url" -v ON_ERROR_STOP=1 -qtA -F '|' -c \
	"SELECT owner, version, name, checksum FROM schema_migrations ORDER BY owner, version" >"$work/ledger.before"
audit_before=$(psql "$db_admin_url" -qtA -c "SELECT checksum FROM schema_migrations WHERE name = '000048_audit_history_append_only.up.sql'")
if [ -z "$audit_before" ]; then
	echo "FAIL  the fresh database records no audit/000048 row, so there is no applied checksum to protect"
	exit 1
fi

# The mutant: the same tree, one comment line on the file the database already has applied.
mutant="$work/mutant"
mkdir -p "$mutant"
git -C "$root" archive HEAD | tar -x -C "$mutant"
applied="modules/audit/migrations/000048_audit_history_append_only.up.sql"
printf '\n-- one line, appended by a hand that did not read the ledger\n' >>"$mutant/$applied"
(cd "$mutant" && go build -buildvcs=false -o "$work/platformkit-mutant" ./apps/platformkit) || {
	echo "FAIL  the mutated tree does not build; nothing was refused"
	exit 1
}

out=$("$work/platformkit-mutant" migrate --config "$work/run.yaml" 2>&1)
status=$?
if [ "$status" -eq 0 ]; then
	fail "an applied migration was rewritten and the runner applied it anyway"
else
	if printf '%s' "$out" | grep -q 'changed after it was applied'; then
		echo "ok    the rewritten applied migration is refused by name"
	else
		echo "FAIL  migrate stopped for another reason than the applied byte change"
		printf '%s\n' "$out" | tail -5 | sed 's/^/      /'
	fi
fi

# And it stopped without writing: the same rows, the same checksum, nothing new.
psql "$db_admin_url" -v ON_ERROR_STOP=1 -qtA -F '|' -c \
	"SELECT owner, version, name, checksum FROM schema_migrations ORDER BY owner, version" >"$work/ledger.after"
if ! diff -q "$work/ledger.before" "$work/ledger.after" >/dev/null; then
	fail "the refusal wrote to the ledger: $(diff "$work/ledger.before" "$work/ledger.after" | head -4 | tr '\n' ' ')"
else
	echo "ok    the refusal wrote no ledger row and changed no stored checksum"
fi
after=$(psql "$db_admin_url" -qtA -c "SELECT checksum FROM schema_migrations WHERE name = '000048_audit_history_append_only.up.sql'")
[ "$after" = "$audit_before" ] &&
	echo "ok    audit/000048 is still recorded under $audit_before" ||
	fail "audit/000048 was recorded under $after, and it was applied under $audit_before"

if [ "$fails" -gt 0 ]; then echo "applied-bytes: $fails case(s) failed"; exit 1; fi
echo "applied-bytes: a rewritten applied migration is refused and the ledger it refused is the ledger it left"
