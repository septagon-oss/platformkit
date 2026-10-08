#!/usr/bin/env bash
# review15_rehearsal_dump_test.sh — the review's case for the one branch of
# scripts/rehearse_migrations.sh no run in this branch's history had taken: `--dump`.
#
# The step has two ways to get its copy. `--base-ref` builds the previous release and
# runs its `bootstrap`; `--dump` restores an operator's `pg_dump`. Every recorded run
# of this step, in the delivery's and in seven reviews' *Not verified*, is a
# `--base-ref` run or an argument refusal — the delivery's own IMPLEMENT.md and each
# review say the `--dump` branch was "covered only by scripts/review_rehearsal_test.sh
# and the make check rehearsal steps, which I saw pass". Those cover the contended grep
# and the watcher's program over a captured log; none restores a dump.
#
# So this runs it end to end, and runs it where it means something: the base is
# 75b04d1^, the revision before the last migration file this repository holds
# (modules/user/migrations/000025_handle.up.sql), so the candidate has exactly one file
# pending, and that file adds a column and builds two indexes over `users` — the table
# scripts/testdata/rehearse/seed.sql fills with ten thousand rows. That is the release
# step's own question — what does this file cost on a table the size the installation
# has — asked of the copy path an operator uses.
#
# What is asserted is what the step exists to produce, read out of the copy rather than
# out of its prose: the copy carried the ledger the base applied and the rows the seed
# wrote, the candidate applied the one pending file against them, the ledger advanced to
# that version, both indexes exist, and no progress row was left.
#
# Nothing in the repository runs this file; `bash scripts/review15_rehearsal_dump_test.sh`
# does. It needs PLATFORMKIT_TEST_ADMIN_URL (the owner connection the step itself insists
# on) and PLATFORMKIT_TEST_DATABASE_URL (the application connection `bootstrap` opens),
# plus pg_dump. It creates and drops only databases named platformkit_rehearse_*.
set -Eeuo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
script="$root/scripts/rehearse_migrations.sh"
base_ref="${REVIEW15_BASE_REF:-75b04d1^}"
failed=0
fail() { echo "FAIL: $*" >&2; failed=1; }
ok() { echo "ok: $*"; }

admin_url="${PLATFORMKIT_TEST_ADMIN_URL:-}"
[ -n "$admin_url" ] || { echo "FAIL: PLATFORMKIT_TEST_ADMIN_URL is unset" >&2; exit 1; }
for tool in psql pg_dump jq; do
	command -v "$tool" >/dev/null || { echo "FAIL: $tool is not installed" >&2; exit 1; }
done

work="$(mktemp -d)"
dbs=()
cleanup() {
	local db
	for db in "${dbs[@]:-}"; do
		case "$db" in
		platformkit_rehearse_*) psql "$admin_url" -q -c "DROP DATABASE IF EXISTS $db" >/dev/null 2>&1 || true ;;
		esac
	done
	rm -rf "$work"
}
trap cleanup EXIT

# One database inside the environment's own URI, everything else as written.
with_database() {
	local rest="${1#*://}" authority tail="" query=""
	if [[ "$rest" == */* ]]; then authority="${rest%%/*}"; tail="${rest#*/}"; else authority="$rest"; fi
	[[ "$tail" == *\?* ]] && query="?${tail#*\?}"
	printf '%s://%s/%s%s' "${1%%://*}" "$authority" "$2" "$query"
}
query() { psql "$(with_database "$admin_url" "$1")" -v ON_ERROR_STOP=1 -qtA -c "$2"; }

# A dump to rehearse with. REVIEW15_DUMP reviews an existing one (stage 3 needs no
# fresh base at all); otherwise stage 1 builds the base and dumps it.
dump="${REVIEW15_DUMP:-$work/base.dump}"

# ---------------------------------------------------------------------------
# 1. A `--base-ref` rehearsal, kept, so the base it built can be dumped.
# ---------------------------------------------------------------------------
if [ -n "${REVIEW15_DUMP:-}" ]; then
	[ -r "$dump" ] || { fail "REVIEW15_DUMP names no readable dump at $dump"; exit 1; }
	ok "stage 1 skipped: rehearsing the existing dump at $dump"
else
if ! timeout 1800 bash "$script" --base-ref "$base_ref" --keep >"$work/baseref.log" 2>&1; then
	cat "$work/baseref.log" >&2
	fail "the --base-ref rehearsal the dump is taken from did not exit 0; nothing below this line means anything"
	exit 1
fi
base_db=$(sed -n 's/^rehearse: kept \(platformkit_rehearse_[a-z0-9_]*\) and .*/\1/p' "$work/baseref.log" | tail -1)
base_run=$(sed -n 's/^rehearse: kept .* and \(platformkit_rehearse_[a-z0-9_]*\)$/\1/p' "$work/baseref.log" | tail -1)
[ -n "$base_db" ] || {
	fail "could not read the base database out of the step's own 'kept' line; the step no longer names what it left behind"
	exit 1
}
dbs+=("$base_db" "$base_run")
grep -q 'ledger the copy starts from:' "$work/baseref.log" ||
	fail "the --base-ref run printed no 'ledger the copy starts from' line; the step no longer reports the ledger before the candidate connects"
ok "the --base-ref run built $base_db and named its ledger"

pg_dump -Fc -f "$dump" "$(with_database "$admin_url" "$base_db")"
[ -s "$dump" ] || { fail "pg_dump of $base_db produced an empty file"; exit 1; }
[ -n "$base_run" ] && psql "$admin_url" -q -c "DROP DATABASE $base_run" >/dev/null
dbs=("${base_db:-}")
fi

# ---------------------------------------------------------------------------
# 2. The branch nobody ran: the same candidate over a restored dump.
# ---------------------------------------------------------------------------
set +e
timeout 1800 bash "$script" --dump "$dump" --keep >"$work/dump.log" 2>&1
code=$?
set -e
run_db=$(sed -n 's/^rehearse: kept \(platformkit_rehearse_[a-z0-9_]*\)$/\1/p' "$work/dump.log" | tail -1)
[ -n "$run_db" ] && dbs+=("$run_db")

if [ "$code" -ne 0 ]; then
	cat "$work/dump.log" >&2
	fail "the --dump rehearsal exited $code; the operator's copy path is not the base-ref path and this case exists to tell them apart"
	exit 1
fi
ok "the --dump rehearsal exited 0"

# It restored, it seeded, and it read the ledger the copy arrived with.
grep -q "restored from $dump" "$work/dump.log" || fail "the step never said it restored the dump"
grep -q 'seeded .* into platformkit_rehearse_' "$work/dump.log" || fail "the step never said it seeded the copy"
ledger=$(grep -o 'ledger the copy starts from: [0-9]* applied version(s)[^,]*, highest version [0-9]*' "$work/dump.log" | tail -1)
[ -n "$ledger" ] || fail "the --dump run printed no base ledger line: the copy's own ledger is the only fact that tells 'this release changes no schema' from 'the base already had these bytes'"
case "$ledger" in
*"highest version 24") ok "the copy said what it arrived with — $ledger" ;;
*) fail "the copy's ledger reported '$ledger'; the base is $base_ref, whose highest applied version is 24, so the number is not the base's" ;;
esac

# The candidate applied the release's one pending file, and said how long it took.
grep -Eq '^user/25 +expand +[0-9]+ ms +000025_handle\.up\.sql$' "$work/dump.log" ||
	fail "the candidate applied no file over the restored copy: $(grep -c 'file(s) applied' "$work/dump.log") report line(s); a rehearsal that applies nothing measures nothing, and this one had a file pending to apply"
grep -q 'rehearse: 1 file(s) applied in' "$work/dump.log" || fail "the step did not report one file applied"
grep -q '^rehearse: ok: 1 file(s) within' "$work/dump.log" || fail "the step did not resolve to its ok line for the one file"

# And the copy really was the copy it claims: the seed's rows, the release's column and
# indexes, the ledger advanced, nothing left mid-drain.
if [ -z "$run_db" ]; then
	fail "the --dump run kept nothing, so the copy could not be inspected; the 'kept' line is the only name the step gives"
else
	rows=$(query "$run_db" "SELECT count(*) FROM users")
	[ "${rows:-0}" -gt 10000 ] || fail "the restored, seeded copy holds $rows users rows: the index build was measured against an empty table, which is what the step says it exists to avoid"
	cols=$(query "$run_db" "SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='users' AND column_name='handle'")
	[ "${cols:-0}" -eq 1 ] || fail "the release's column is not in the copy the candidate applied it to"
	idx=$(query "$run_db" "SELECT count(*) FROM pg_indexes WHERE schemaname='public' AND indexname IN ('users_tenant_handle','users_tenant_handle_lookup')")
	[ "${idx:-0}" -eq 2 ] || fail "the release's two index builds are not both in the copy ($idx of 2)"
	ver=$(query "$run_db" "SELECT max(version) FROM schema_migrations")
	[ "${ver:-0}" -eq 25 ] || fail "the copy's ledger stops at version $ver, not 25: the file the step reported applying is not in the ledger it left"
	left=$(query "$run_db" "SELECT count(*) FROM schema_migration_backfill")
	[ "${left:-0}" -eq 0 ] || fail "$left progress row(s) left in the copy after a run the step called ok"
	psql "$admin_url" -q -c "DROP DATABASE $run_db" >/dev/null
fi

# ---------------------------------------------------------------------------
# 3. The same dump on a cluster that has no application role — optional, because
#    the role is cluster-wide and the task's own cluster has it.
#
# scripts/rehearse_migrations.sh says of itself: "`--dump` is an operator's
# `pg_dump` of the real database … restored under this cluster's own role rather
# than the roles the dump names, because a rehearsal cluster is not the production
# cluster's role directory", and its `apply_defaults` says "a copy whose new tables
# the application role cannot reach is not the copy the step claims to be
# measuring". But `apply_defaults` starts `role_exists || return 0`, and the only
# `create_role` call in the script sits in the `--base-ref` branch — the branch an
# operator does not use when they have a dump. On a cluster with no
# `platformkit_app`, `--dump` therefore restores, seeds, applies the release and
# prints `ok:` over a copy with no application role, no default privileges and no
# word about either.
#
# Give this case PLATFORMKIT_NO_ROLE_ADMIN_URL (an owner URL into a cluster whose
# `platformkit_app` role is absent — `initdb -U postgres` is one) and it asks the
# step which of the two honest answers it gives: create the role, as the base-ref
# branch does, or say the copy has none. Reporting `ok:` and nothing else is the
# answer it must not give.
# ---------------------------------------------------------------------------
if [ -n "${PLATFORMKIT_NO_ROLE_ADMIN_URL:-}" ]; then
	url="${PLATFORMKIT_NO_ROLE_ADMIN_URL}"
	if [ -n "$(psql "$url" -qtA -c "SELECT 1 FROM pg_roles WHERE rolname = 'platformkit_app'")" ]; then
		ok "PLATFORMKIT_NO_ROLE_ADMIN_URL's cluster has the application role; stage 3 needs a cluster without it and will not report a false answer"
	else
		set +e
		PLATFORMKIT_TEST_ADMIN_URL="$url" timeout 1800 bash "$script" --dump "$dump" --keep >"$work/norole.log" 2>&1
		nocode=$?
		set -e
		norole_run=$(sed -n 's/^rehearse: kept \(platformkit_rehearse_[a-z0-9_]*\)$/\1/p' "$work/norole.log" | tail -1)
		[ -n "$norole_run" ] && dbs+=("$norole_run")
		grants=0
		if [ -n "$norole_run" ]; then
			grants=$(psql "$(with_database "$url" "$norole_run")" -qtA -c "SELECT count(*) FROM pg_default_acl")
			psql "$url" -q -c "DROP DATABASE $norole_run" >/dev/null 2>&1 || true
		fi
		if [ "$nocode" -eq 2 ] || grep -qi 'application role\|platformkit_app' "$work/norole.log"; then
			ok "the step answered a cluster with no application role with a refusal or a sentence about it"
		elif [ "$nocode" -eq 0 ] && [ "${grants:-0}" -eq 0 ]; then
			cat "$work/norole.log" >&2
			fail "--dump reported ok: over a copy with no platformkit_app role and $grants default-privilege rows, and said nothing about it: the step's own comment calls that copy 'not the copy the step claims to be measuring'. Create the role the way --base-ref does, or name the copy's missing role in the report"
		else
			fail "the role-less --dump run exited $nocode with $grants default-privilege rows; this case cannot read that answer and says so"
		fi
	fi
fi

[ "$failed" = 0 ] && echo "review15: the rehearsal's --dump path restores, seeds, applies and reports"
exit "$failed"
