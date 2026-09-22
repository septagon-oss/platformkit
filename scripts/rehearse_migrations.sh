#!/usr/bin/env bash
# The rehearsal: apply this tree's pending migrations to a *copy* of a
# production-shaped database, and report what each one cost.
#
#	scripts/rehearse_migrations.sh (--dump FILE | --base-ref REF)
#	                              [--seed FILE] [--max-file-seconds N]
#	                              [--max-lock-ms N] [--keep]
#
# ADR 0011 and migrations/README.md make this the step a release requires: the
# runner refuses a shape of statement, a `lock_timeout` bounds a wait, and neither
# says what the release will cost on the size of table the installation actually
# has. This measures it, on a copy, before the version is published.
#
# Where the copy comes from is one of two things, and the script refuses both or
# neither. `--dump` is an operator's `pg_dump` of the real database — custom,
# directory or plain format — restored under this cluster's own role rather than the
# roles the dump names, because a rehearsal cluster is not the production cluster's
# role directory. `--base-ref REF` builds the previous release's *own binary* out of
# that revision and runs its `bootstrap`, which migrates the ledger with that
# release's runner and creates the one tenant the seed file then fills: the real code
# path at both ends, no runner reimplemented in bash. The seed
# (`scripts/testdata/rehearse/seed.sql`, ten thousand rows in each table it names)
# lands in the copy, where the migrations then run; CI has no production database,
# and a migration measured against an empty table is a migration measured against
# nothing.
#
# Exit codes are part of the interface, because a release pipeline has to tell the
# three failures apart:
#
#	0  everything pending applied, inside both budgets
#	1  a migration failed (a rule refusal lands here, with the rule's own message)
#	2  the rehearsal could not run: no tools, no base, bad arguments
#	3  a budget was exceeded, or a migration came back contended
#
# A contended migration is a finding and never a pass: discovering it is the whole
# point of doing this before the release rather than during it.
#
# It creates exactly two databases, `platformkit_rehearse_base_*` and
# `platformkit_rehearse_run_*`, drops both on the way out unless `--keep` says
# otherwise, and refuses to drop any name without one of those two prefixes. It
# never touches the schemas `make down` owns.
#
# What it does not measure: the wait behind a table a running application is
# reading — there is no application here, and lock waits are *sampled* every
# 100 ms, so a wait shorter than the interval can be missed. Both numbers are
# reported as what they are.
set -Eeuo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

# The resolution the lock watcher samples at, and the name it samples on: only
# this run's migrate session carries it, which is why it is in the DSN.
SAMPLE_MS=100
APPNAME=platformkit-rehearse
prefix="platformkit_rehearse_"

die() { local code="$1"; shift; echo "rehearse: $*" >&2; exit "$code"; }
note() { echo "rehearse: $*"; }
usage() { echo "usage: rehearse_migrations.sh (--dump FILE | --base-ref REF) [--seed FILE] [--max-file-seconds N] [--max-lock-ms N] [--keep]"; }

dump="${REHEARSE_DUMP:-}"
ref="${REHEARSE_BASE_REF:-}"
seed="${REHEARSE_SEED-$root/scripts/testdata/rehearse/seed.sql}"
max_file="${REHEARSE_MAX_FILE_SECONDS:-30}"
max_lock="${REHEARSE_MAX_LOCK_MS:-5000}"
keep=0
while [ $# -gt 0 ]; do
	case "$1" in
	--dump) [ $# -ge 2 ] || die 2 "--dump wants a file"; dump="$2"; shift 2 ;;
	--base-ref) [ $# -ge 2 ] || die 2 "--base-ref wants a revision"; ref="$2"; shift 2 ;;
	--seed) [ $# -ge 2 ] || die 2 "--seed wants a file (or the empty string for none)"; seed="$2"; shift 2 ;;
	--max-file-seconds) [ $# -ge 2 ] || die 2 "--max-file-seconds wants a number"; max_file="$2"; shift 2 ;;
	--max-lock-ms) [ $# -ge 2 ] || die 2 "--max-lock-ms wants a number"; max_lock="$2"; shift 2 ;;
	--keep) keep=1; shift ;;
	-h | --help) usage; exit 0 ;;
	*) die 2 "unknown argument $1; --help lists them" ;;
	esac
done

# Argument refusals come first, so they are answerable on a machine with no
# database and no client tools — which is also what makes them testable.
[ -n "$dump" ] || [ -n "$ref" ] || die 2 "say where the copy comes from: --dump FILE or --base-ref REF"
[ -z "$dump" ] || [ -z "$ref" ] || die 2 "--dump and --base-ref are alternatives; the copy is either an operator's dump or the previous release's own migration"
if [ -n "$dump" ] && [ ! -r "$dump" ]; then die 2 "no dump readable at $dump"; fi
if [ -n "$ref" ] && ! git rev-parse --verify --quiet "${ref}^{commit}" >/dev/null; then
	die 2 "$ref does not name a revision of this repository"
fi
[[ "$max_file" =~ ^[0-9]+$ ]] || die 2 "--max-file-seconds is $max_file, which is not a number of seconds"
[[ "$max_lock" =~ ^[0-9]+$ ]] || die 2 "--max-lock-ms is $max_lock, which is not a number of milliseconds"
if [ -n "$seed" ] && [ ! -r "$seed" ]; then die 2 "no seed file readable at $seed (pass --seed '' to seed nothing)"; fi

admin_url="${PLATFORMKIT_TEST_ADMIN_URL:-}"
if [ -z "$admin_url" ]; then
	die 2 "PLATFORMKIT_TEST_ADMIN_URL is unset: it is the owner connection this step creates and drops its two databases through"
fi
missing=(psql go jq git)
if [ -n "$dump" ]; then missing+=(pg_restore); fi
absent=()
for tool in "${missing[@]}"; do
	command -v "$tool" >/dev/null || absent+=("$tool")
done
if [ ${#absent[@]} -gt 0 ]; then
	die 2 "not installed: ${absent[*]} (the rehearsal fails loudly rather than passing quietly)"
fi
case "$admin_url" in
*://*) ;;
*) die 2 "PLATFORMKIT_TEST_ADMIN_URL must be a postgres:// URI; a rehearsal needs to name a database inside it" ;;
esac

# One database name inside the same URI, everything else — host, port, credentials,
# options — exactly as the environment wrote it.
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
with_application_name() {
	# ? for a URI that named no options, & for one that did: the environment's own
	# URL usually carries sslmode, and a rehearsal built on the wrong separator is a
	# rehearsal that could not connect at all.
	local sep='?'
	[[ "$1" == *\?* ]] && sep='&'
	printf '%s%sapplication_name=%s' "$1" "$sep" "$APPNAME"
}
psql_as() { psql "$(with_database "$admin_url" "$1")" -v ON_ERROR_STOP=1 -qtA "${@:2}"; }
drop_database() {
	case "$1" in
	"$prefix"*) psql "$admin_url" -q -c "DROP DATABASE IF EXISTS $1" ;;
	*) echo "rehearse: refusing to drop $1: it is not this step's" >&2 ;;
	esac
}
# The configuration the two binaries are handed. `bootstrap` opens the application
# connection as well as the owner one — it writes a tenant — so the app URL has to be
# the unprivileged role row-level security binds, which is why kit/db refuses to open
# a superuser's connection for it. The migration step opens no application connection
# at all, so a --dump rehearsal needs only the owner URL.
write_config() {
	cat >"$1" <<YAML
server:
  # Nothing listens: this step runs migrate and exits.
  addr: ":0"
  public_host: "rehearse.localhost"
database:
  url: "$2"
  migrate_url: "$3"
nats:
  # Required and unused: the events transport is chosen by role, and no role
  # that needs one runs here.
  url: "nats://127.0.0.1:4222"
log:
  # info, which is where the runner says what it applied and how long it took.
  level: "info"
YAML
}

stamp="$(date +%s)_${RANDOM}_$$"
base=""
[ -n "$ref" ] && base="${prefix}base_$stamp"
run="${prefix}run_$stamp"
work="$(mktemp -d)"
waits="$work/waits"
: >"$waits"
watcher=""

finish() {
	local code=$?
	trap - EXIT
	if [ -n "$watcher" ]; then
		kill "$watcher" 2>/dev/null || true
		wait "$watcher" 2>/dev/null || true
	fi
	if [ "$keep" = 1 ]; then
		note "kept ${base:+$base and }$run"
	else
		[ -n "$base" ] && drop_database "$base" >/dev/null 2>&1 || true
		drop_database "$run" >/dev/null 2>&1 || true
	fi
	rm -rf "$work"
	exit "$code"
}
trap finish EXIT

# The role and grant bootstrap file, applied in two halves. `CREATE ROLE` is
# cluster-wide and fails the second time (start.go checks pg_roles for the same
# reason), so it runs once, on the maintenance database, and only when the role is
# missing. The rest is per-database — ALTER DEFAULT PRIVILEGES lives inside one
# database — and has to land before any migration creates a table, or the copy has
# tables the application role cannot reach and is not the copy it claims to be.
init_file="$root/apps/platformkit/postgres-init.sql"
role_exists() { [ -n "$(psql "$admin_url" -qtA -c "SELECT 1 FROM pg_roles WHERE rolname = 'platformkit_app'")" ]; }
# The role is cluster-wide, so it is created once, on the database the environment
# names, and only when it is missing: the file's CREATE ROLE has no IF NOT EXISTS and
# the second run failing on it would be a rehearsal that refuses to run.
create_role() {
	[ -r "$init_file" ] || die 2 "no $init_file to create the application role from"
	psql "$admin_url" -v ON_ERROR_STOP=1 -q -f "$init_file"
}
# The rest of that file, one statement at a time: ALTER DEFAULT PRIVILEGES lives
# inside a database, so a database this step created has none until it runs, and a
# copy whose new tables the application role cannot reach is not the copy the step
# claims to be measuring. The CREATE ROLE line is dropped here because create_role
# owns it, and the split is the same one start.go's own statements() makes.
apply_defaults() {
	local target statement
	role_exists || return 0
	target="$(with_database "$admin_url" "$1")"
	while IFS= read -r statement; do
		psql "$target" -v ON_ERROR_STOP=1 -q -c "$statement"
	done < <(grep -v '^[[:space:]]*--' "$init_file" | tr '\n' ' ' | tr ';' '\n' |
		sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' -e '/^$/d' -e '/^CREATE ROLE/d')
}

restore() { # $1 = database, $2 = dump file (custom format or plain SQL)
	local url script; url="$(with_database "$admin_url" "$1")"
	if [ "$(head -c 5 "$2")" = "PGDMP" ]; then
		# A custom-format dump is turned into a script and run by psql rather than
		# restored over a connection. Measured reason: a client newer than the server
		# adds its *own* session settings to the restore it runs — pg_restore 18 emits
		# `SET transaction_timeout = 0`, which PostgreSQL 16 refuses as an unrecognised
		# parameter, so the step would die before the release's first statement — and a
		# rehearsal that dies on its own tooling is a step that gets switched off. The
		# line is dropped, said out loud, and every other statement error still stops
		# the step, because ON_ERROR_STOP is on.
		script="$work/restore.sql"
		pg_restore --no-owner --no-privileges -f "$script" "$2"
		if grep -q '^SET transaction_timeout = 0;$' "$script"; then
			grep -v '^SET transaction_timeout = 0;$' "$script" >"$script.filtered"
			mv "$script.filtered" "$script"
			note "dropped the restoring client's own SET transaction_timeout: the server does not know that setting"
		fi
		psql "$url" -v ON_ERROR_STOP=1 -q -f "$script"
	else
		psql "$url" -v ON_ERROR_STOP=1 -q -f "$2"
	fi
}

note "candidate: $(git rev-parse --short HEAD)$(git diff --quiet || echo ' with a dirty tree')"
go build -o "$work/platformkit" ./apps/platformkit || die 2 "this tree does not build, so there is nothing to rehearse"

if [ -n "$base" ]; then
	note "base: $ref in $base, migrated and seeded"
	app_url="${PLATFORMKIT_TEST_DATABASE_URL:-}"
	if [ -z "$app_url" ]; then
		die 2 "PLATFORMKIT_TEST_DATABASE_URL is unset: --base-ref bootstraps a tenant over the application connection, and kit/db will not open one as a superuser"
	fi
	psql "$admin_url" -q -c "CREATE DATABASE $base"
	role_exists || create_role
	apply_defaults "$base"
	src="$work/base-tree"
	mkdir -p "$src"
	git archive "$ref" | tar -x -C "$src"
	(cd "$src" && go build -o "$work/platformkit-base" ./apps/platformkit) ||
		die 2 "the base revision does not build; the copy has to be made by that release's own code, not by this one's"
	write_config "$work/base.yaml" "$(with_database "$app_url" "$base")" "$(with_database "$admin_url" "$base")"
	# bootstrap migrates the whole ledger with that revision's runner and then
	# creates the first tenant, which is the row the seed file hangs its rows off.
	if ! "$work/platformkit-base" bootstrap --config "$work/base.yaml" \
		--tenant rehearse --host rehearse.localhost --name Rehearsal \
		--admin-email rehearse@rehearse.localhost >"$work/base.log" 2>&1; then
		tail -20 "$work/base.log" >&2
		die 2 "the base revision could not migrate $base; it needs the migrate or bootstrap command (this one arrived with the rehearsal step)"
	fi
else
	note "base: the operator's dump at $dump"
fi

if [ -n "$dump" ]; then
	note "copy: $run, restored from $dump"
	psql "$admin_url" -q -c "CREATE DATABASE $run"
	apply_defaults "$run"
	restore "$run" "$dump"
else
	# The copy is a file-level copy of the base rather than a dump and a restore.
	# Measured reason, not taste: a client newer than the server writes a dump the
	# server refuses to restore (pg_dump 18 emits `SET transaction_timeout = 0`,
	# which PostgreSQL 16 rejects as an unrecognized parameter), and a rehearsal
	# that died on its own tooling would be a rehearsal nobody runs. TEMPLATE copies
	# the database exactly, and the base stays behind untouched for `--keep`.
	note "copy: $run, a file-level copy of $base"
	psql "$admin_url" -q -c "CREATE DATABASE $run TEMPLATE $base"
fi
if [ -n "$seed" ]; then
	psql_as "$run" -f "$seed" >/dev/null
	note "seeded $seed into $run"
fi

# The watcher: one session, one query, sampled every SAMPLE_MS milliseconds, and
# only this run's session carries the application name it filters on.
run_url="$(with_application_name "$(with_database "$admin_url" "$run")")"
psql "$run_url" -At -o "$waits" \
	-c "SELECT count(*) FROM pg_stat_activity WHERE application_name = '$APPNAME' AND wait_event_type = 'Lock'" \
	-c '\watch 0.1' >/dev/null 2>&1 &
watcher=$!

write_config "$work/run.yaml" "${app_url:-$run_url}" "$run_url"
set +e
"$work/platformkit" migrate --drain --config "$work/run.yaml" >"$work/run.log" 2>&1
code=$?
set -e
kill "$watcher" 2>/dev/null || true
wait "$watcher" 2>/dev/null || true
watcher=""

# Durations come from the runner's own lines, not from an estimate around the
# process: the runner timed the file, and nothing outside the transaction knows
# where a file began.
jq -R -r 'fromjson? // empty
	| select(.msg == "db: applied migration" or .msg == "db: drained data migration")
	| [.owner, (.version|tostring), .name, (.phase // "drain"), (.duration_ms|tostring)] | @tsv' \
	"$work/run.log" >"$work/files.tsv"

findings=0
total=0
longest_ms=0
longest="-"
note "one line per file this run applied, with the duration the runner measured"
while IFS=$'\t' read -r owner version name phase ms; do
	[ -n "$owner" ] || continue
	printf '%-22s %-8s %8s ms  %s\n' "$owner/$version" "$phase" "$ms" "$name"
	total=$((total + ms))
	if [ "$ms" -gt "$longest_ms" ]; then
		longest_ms="$ms"
		longest="$owner/$version"
	fi
	if [ "$ms" -gt $((max_file * 1000)) ]; then
		echo "TOO SLOW $owner/$version ${ms}ms > ${max_file}s"
		findings=$((findings + 1))
	fi
done <"$work/files.tsv"

samples=$(grep -c '^[1-9]' "$waits" 2>/dev/null || true)
lock_ms=$((samples * SAMPLE_MS))
if [ "$lock_ms" -gt "$max_lock" ]; then
	echo "LOCK WAIT ${lock_ms}ms > ${max_lock}ms"
	findings=$((findings + 1))
fi

contended=$(grep -o 'db: migrate: [^\n]*contended[^\n]*' "$work/run.log" | tail -1 || true)
if [ -n "$contended" ]; then
	echo "CONTENDED $contended"
	findings=$((findings + 1))
	code=3
fi

# The invariant the two runner tables hold: a completed drain leaves no progress
# row. Anything here is a backfill the release did not finish.
unfinished=$(psql_as "$run" -c "SELECT coalesce(string_agg(owner || '/' || version, ', ' ORDER BY owner, version), '') FROM schema_migration_backfill")
if [ -n "$unfinished" ]; then
	echo "DRAIN UNFINISHED $unfinished"
	findings=$((findings + 1))
fi

applied=$(wc -l <"$work/files.tsv")
note "$applied file(s) applied in ${total}ms; longest $longest at ${longest_ms}ms"
note "lock waits: $samples sample(s) of ${SAMPLE_MS}ms ≈ ${lock_ms}ms (sampled, so a wait shorter than ${SAMPLE_MS}ms can be missed)"
if [ "$code" -ne 0 ]; then
	tail -20 "$work/run.log" >&2
	note "the migration failed; nothing past it applied"
	[ "$findings" -gt 0 ] || code=1
fi
if [ "$findings" -gt 0 ] && [ "$code" -eq 0 ]; then code=3; fi
if [ "$code" -eq 0 ]; then
	note "ok: ${applied} file(s) within ${max_file}s each and ${max_lock}ms of sampled lock waits, against a copy of ${base:-the dump}"
else
	note "failed: $findings finding(s); exit $code"
fi
exit "$code"
