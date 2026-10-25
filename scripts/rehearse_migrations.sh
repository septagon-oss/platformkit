#!/usr/bin/env bash
# The rehearsal: apply this tree's pending migrations to a *copy* of a
# production-shaped database, and report what each one cost.
#
#	scripts/rehearse_migrations.sh (--dump FILE | --base-ref REF | --supported)
#	                              [--seed FILE] [--max-file-seconds N]
#	                              [--max-lock-ms N] [--keep]
#	scripts/rehearse_migrations.sh --check-source REF
#
# `--supported` is the mode a release is certified by, and it is
# scripts/rehearse_supported_matrix.sh: this step once per supported source in
# scripts/testdata/rehearse/supported-sources.json, into one aggregated receipt. Each
# case is this file with that revision as its --base-ref, so the copy, the fixtures,
# the drain, the watcher and both budgets are the ones a single case measures and the
# mode carries no second implementation of them. `--check-source REF` asks the support
# record about one revision and touches no database.
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
# role directory. That last clause is a promise about the cluster, so the step
# holds it: a cluster with no `platformkit_app` gets the role from
# `apps/platformkit/postgres-init.sql` before its copy is restored, whichever way
# the copy arrived, and says so in its report. `--base-ref REF` builds the
# previous release's *own binary* out of that revision and runs its `bootstrap`,
# which migrates the ledger with that release's runner and creates the one tenant
# the seed file then fills: the real code
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
#	1  a migration failed (a rule refusal lands here, with the rule's own message,
#	   which this step prints as soon as the candidate stops — before any query of
#	   its own can fail over a copy the candidate never migrated)
#	2  the rehearsal could not run: no tools, no base, bad arguments, or its own
#	   lock-wait watcher never sampled (a number it did not take is not a pass)
#	3  a budget was exceeded, or a migration came back contended
#
# A contended migration is a finding and never a pass: discovering it is the whole
# point of doing this before the release rather than during it.
#
# It creates exactly two databases, `platformkit_rehearse_base_*` and
# `platformkit_rehearse_run_*`, drops both on the way out unless `--keep` says
# otherwise, refuses to drop any name without one of those two prefixes, and says
# `LEFT BEHIND <name>` out loud when a drop is refused — a copy of a production-shaped
# database that stays is the size of the installation and nobody's business until it
# is named. It never touches the schemas `make down` owns.
#
# What it does not measure: the wait behind a table a running application is
# reading — there is no application here, and lock waits are *sampled* every
# 100 ms, so a wait shorter than the interval can be missed. That is the only gap
# left open: a watcher whose sample file fell short of half the samples the window
# it was actually alive for resolves to is reported as LOCK WATCH BROKEN and exits
# 2, because "0 sample(s) ~ 0ms" of a run that lasted a minute is a measurement
# that never happened. The window is the watcher's own lifetime in milliseconds,
# less the second it is given to connect: measured reason — the floor used to be
# built from the candidate's wall clock rounded up to whole seconds, and a clean run
# that applied two files in 55 ms but straddled a second boundary was then asked for
# five samples its watcher was never alive to take, which made the step report exit
# 2 over a release that had passed. A step that invents a failure it did not measure
# is the same fault as one that reports a 0 it did not measure.
# Both numbers are reported as what they are.
set -Eeuo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

# The resolution the lock watcher samples at, and the name it samples on: only
# this run's migrate session carries it, which is why it is in the DSN. psql's \watch
# takes seconds and allows a fraction, so the interval it is given is derived from
# SAMPLE_MS rather than written beside it: two numbers for one interval drift, and a
# step that samples at one rate and reports the product of another reports a
# measurement it did not take.
SAMPLE_MS=100
SAMPLE_S=$(printf '%d.%03d' "$((SAMPLE_MS / 1000))" "$((SAMPLE_MS % 1000))")
# How long a watcher may take to connect and answer its first sample before its
# silence means anything. A local connect and one query measure 16ms here, but a
# rehearsal runs on a machine that may be busy with the build it just did, so this
# is a second and not 16ms: it costs the floor its opinion about sub-second runs,
# and keeps the one it has about a run of several seconds whose sample file holds
# one line.
WATCH_STARTUP_GRACE_MS=1000

# now_ms is a millisecond clock. Whole seconds are what the watcher's floor used to
# be built from, and rounding a 55ms run up to "1s" asks for samples nobody took.
# A `date` without %N (non-GNU) prints the directive literally, which the check
# below turns into seconds-with-milliseconds rather than a nonsense window.
now_ms() {
	local n
	n=$(date +%s%3N 2>/dev/null) || n=""
	[[ "$n" =~ ^[0-9]+$ ]] || n="$(date +%s)000"
	printf '%s' "$n"
}

# watch_floor is the fewest sample lines a watcher that ran could have left in the
# file over a window that long: half of the intervals after its startup, because a
# machine under load loses samples, and a step that guesses a failure from a lost
# sample is as wrong as one that reports a wait it never saw.
watch_floor() {
	local watched_ms="$1"
	if [ "$watched_ms" -le "$WATCH_STARTUP_GRACE_MS" ]; then
		echo 0
	else
		echo $(( (watched_ms - WATCH_STARTUP_GRACE_MS) / SAMPLE_MS / 2 ))
	fi
}
APPNAME=platformkit-rehearse
prefix="platformkit_rehearse_"

die() { local code="$1"; shift; echo "rehearse: $*" >&2; exit "$code"; }
note() { echo "rehearse: $*"; }
usage() { echo "usage: rehearse_migrations.sh (--dump FILE | --base-ref REF | --supported) [--seed FILE] [--max-file-seconds N] [--max-lock-ms N] [--keep]; or --check-source REF"; }

dump="${REHEARSE_DUMP:-}"
ref="${REHEARSE_BASE_REF:-}"
seed="${REHEARSE_SEED-$root/scripts/testdata/rehearse/seed.sql}"
populated="${REHEARSE_POPULATED-$root/scripts/testdata/rehearse/populated.sql}"
declaration="${REHEARSE_DECLARATION-$root/scripts/testdata/rehearse/candidate.yaml}"
supported=0
check_source=""
seed_given=0
subset_given=""
max_file="${REHEARSE_MAX_FILE_SECONDS:-30}"
max_lock="${REHEARSE_MAX_LOCK_MS:-5000}"
keep=0
while [ $# -gt 0 ]; do
	case "$1" in
	--dump) [ $# -ge 2 ] || die 2 "--dump wants a file"; dump="$2"; subset_given="--dump"; shift 2 ;;
	--base-ref) [ $# -ge 2 ] || die 2 "--base-ref wants a revision"; ref="$2"; subset_given="--base-ref"; shift 2 ;;
	--seed) [ $# -ge 2 ] || die 2 "--seed wants a file (or the empty string for none)"; seed="$2"; seed_given=1; shift 2 ;;
	--supported) supported=1; shift ;;
	--check-source) [ $# -ge 2 ] || die 2 "--check-source wants a revision"; check_source="$2"; shift 2 ;;
	--max-file-seconds) [ $# -ge 2 ] || die 2 "--max-file-seconds wants a number"; max_file="$2"; shift 2 ;;
	--max-lock-ms) [ $# -ge 2 ] || die 2 "--max-lock-ms wants a number"; max_lock="$2"; shift 2 ;;
	--keep) keep=1; shift ;;
	-h | --help) usage; exit 0 ;;
	*) die 2 "unknown argument $1; --help lists them" ;;
	esac
done

# Argument refusals come first, so they are answerable on a machine with no
# database and no client tools — which is also what makes them testable.
# The support record answers this before any database is involved: a revision that is
# not supported gets the record's refusal, and a revision that is named by no one gets
# the sentence that says so. Both are exit 1, because the refusal is the answer.
if [ -n "$check_source" ]; then
	"$root/scripts/supported_sources.sh" verdict "$check_source"
	exit $?
fi

if [ "$supported" = 1 ]; then
	[ "$#" -eq 0 ] || die 2 "supported mode takes no per-case arguments; $1 would rehearse a subset"
	[ -z "$subset_given" ] || die 2 "supported mode is certified by the whole matrix; $subset_given names one case and a receipt for one case certifies nothing"
	[ "$seed_given" -eq 0 ] || die 2 "supported mode seeds every case from scripts/testdata/rehearse/seed.sql; --seed replaces the data the promise is about"
	[ "$keep" -eq 0 ] || die 2 "supported mode drops each case's copies itself; --keep would leave one pair per supported source behind"
fi

[ -n "$dump" ] || [ -n "$ref" ] || [ "$supported" = 1 ] ||
	die 2 "say where the copy comes from: --dump FILE, --base-ref REF, or --supported for every source the release promises"
[ -z "$dump" ] || [ -z "$ref" ] || die 2 "--dump and --base-ref are alternatives; the copy is either an operator's dump or the previous release's own migration"
if [ -n "$dump" ] && [ ! -r "$dump" ]; then die 2 "no dump readable at $dump"; fi
if [ -n "$ref" ] && ! git rev-parse --verify --quiet "${ref}^{commit}" >/dev/null; then
	die 2 "$ref does not name a revision of this repository"
fi
[[ "$max_file" =~ ^[0-9]+$ ]] || die 2 "--max-file-seconds is $max_file, which is not a number of seconds"
[[ "$max_lock" =~ ^[0-9]+$ ]] || die 2 "--max-lock-ms is $max_lock, which is not a number of milliseconds"
if [ -n "$seed" ] && [ ! -r "$seed" ]; then die 2 "no seed file readable at $seed (pass --seed '' to seed nothing)"; fi
if [ -n "$populated" ] && [ ! -r "$populated" ]; then
	die 2 "no populated fixture readable at $populated (pass REHEARSE_POPULATED='' to seed volume only)"
fi
if [ ! -r "$declaration" ]; then
	die 2 "no candidate declaration readable at $declaration: the release under test would boot with no hosts named and place nothing"
fi

# The matrix is this step run once per supported source, and nothing else: the copy, the
# fixtures, the drain, the lock watcher and both budgets are the ones a single case
# already measures, so the mode adds no second implementation of them to keep honest.
# What it adds is the obligation that every source the release promises was run, and that
# each refused source was refused out loud.
#
# The hand-over is an exec, and it is here rather than further down because `exec` replaces
# this shell and this shell owns the EXIT trap that removes $work: a mode that reached its
# temp directory before the exec would leave one behind on every run. Every check a case
# would make is made above this line or by the matrix itself, which names the same tools
# and the same owner URL it refuses without.
if [ "$supported" = 1 ]; then
	# The two fixtures are part of the promise: scripts/supported_sources.sh owes them by
	# digest and scripts/check_pin_rehearsal.sh --strict refuses a case that seeded anything
	# else. An environment that swaps one out would make every case measure a database no
	# installation holds, so the mode refuses the environment rather than noticing it in the
	# receipt, downstream of the run that wasted the copy.
	[ -z "${REHEARSE_SEED+set}" ] ||
		die 2 "supported mode seeds the fixture the support record owes; REHEARSE_SEED replaces the data the promise is about"
	[ -z "${REHEARSE_POPULATED+set}" ] ||
		die 2 "supported mode seeds the fixture the support record owes; REHEARSE_POPULATED replaces the second tenant, the page text and the queued events the upgrade has to carry"
	exec "$root/scripts/rehearse_supported_matrix.sh"
fi

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
# A copy that will not drop stays behind, and says which one it is. Measured reason
# this is not quiet: a run whose own session was still attached to its copy had its
# DROP refused and said nothing at all, and a step that makes databases in a
# production-shaped cluster cannot afford to lose one without a word — the leftover
# is the size of the installation, and it is nobody's until somebody notices it.
leave_or_drop() {
	local error
	if error=$(drop_database "$1" 2>&1); then
		return 0
	fi
	echo "rehearse: LEFT BEHIND $1: the copy could not be dropped ($(printf '%s' "$error" | head -1))"
}
# The configuration the two binaries are handed. `bootstrap` opens the application
# connection as well as the owner one — it writes a tenant — so the app URL has to be
# the unprivileged role row-level security binds, which is why kit/db refuses to open
# a superuser's connection for it. The migration step opens no application connection
# at all, so a --dump rehearsal needs only the owner URL.
#
# The fourth argument, when given, is appended verbatim. Only the candidate is handed
# one: the base revision decodes its configuration with KnownFields on and would
# refuse a key that release never had, so what this release declares stays out of
# that file.
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
	if [ -n "${4:-}" ]; then
		printf '%s\n' "$4" >>"$1"
	fi
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
		[ -n "$base" ] && leave_or_drop "$base" || true
		leave_or_drop "$run" || true
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
	psql "$admin_url" -v ON_ERROR_STOP=1 -q -f "$init_file" ||
		die 2 "the application role could not be created from $init_file: the copy would hold tables no application connection can reach"
	note "created the application role platformkit_app from ${init_file#"$root/"} on this cluster: every copy this step measures is restored and migrated under that role, and a cluster without it has no such role to restore under"
}
# The rest of that file, one statement at a time: ALTER DEFAULT PRIVILEGES lives
# inside a database, so a database this step created has none until it runs, and a
# copy whose new tables the application role cannot reach is not the copy the step
# claims to be measuring. The CREATE ROLE line is dropped here because create_role
# owns it, and the split is the same one start.go's own statements() makes.
apply_defaults() {
	local target statement
	# The silent `|| return 0` that stood here was the whole of the fault `--dump` carried:
	# on a cluster with no platformkit_app it skipped every grant, and the step restored,
	# seeded, applied the release and printed `ok:` over a copy no application connection
	# could reach — the copy this comment has always said the step is not measuring.
	# `scripts/rehearsal_dump_test.sh` stage 3 runs exactly that copy. Both
	# branches create the role before they call this, so a role missing by now is that
	# promise broken, and a step that cannot build the copy it reports on exits 2.
	role_exists || die 2 "no platformkit_app role on this cluster and none created: refusing to measure a copy whose tables the application role cannot reach"
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

# Measured reason for `git status --porcelain` rather than `git diff --quiet`: `git diff`
# compares tracked content, and `go build` on the next line compiles what is not tracked
# either. A migration file its author has just written and not yet committed — the exact
# file this step exists to price — was then applied to the copy by the binary the step
# built, and reported under the name of a revision that does not contain it. The report
# is the artefact a release reads, so it names the tree the run was built from, and
# `scripts/rehearsal_provenance_test.sh` runs this line over a repository holding
# one committed file and one uncommitted one. The test is on the captured text rather
# than `| grep -q .`: this script runs under `pipefail`, and a `git status` whose output
# fills the pipe before `grep -q` has its answer dies of SIGPIPE (141), which `&&` reads
# as "clean" — measured over a tree of 5000 untracked files, which is a working tree mid
# refactor and reported itself as clean.
note "candidate: $(git rev-parse --short HEAD)$(test -n "$(git status --porcelain)" && echo ' with a dirty tree')"
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
	# -buildvcs=false, because the exported tree carries no .git of its own and Go's
	# stamping step dies before the compiler starts: `git rev-parse --git-dir` in the
	# copy answers "not a git repository" with 128, which `go build` reports as
	# "error obtaining VCS status" and refuses (measured on v1.1.0 on this machine,
	# 2026-10-01 — the step had never been reached outside a working tree). The base
	# binary exists to migrate and bootstrap the copy with that release's own runner;
	# a build stamp naming *this* checkout would misreport which release built it.
	(cd "$src" && go build -buildvcs=false -o "$work/platformkit-base" ./apps/platformkit) ||
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
	# The role before the copy, exactly as --base-ref does it: the header promises a dump
	# "restored under this cluster's own role", and an operator who has a dump never takes
	# the branch that used to create the role, so on a cluster without one the copy was
	# restored under no application role at all, held no default privileges, and the report
	# said nothing about either.
	role_exists || create_role
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
# The second fixture is the one the promise is about: a second tenant, page text, file
# records and queued events, so the upgrade is measured against an installation and not
# against a schema. It lands on the copy at the *base* revision's shape, which is what
# makes it evidence — SQL this tree wrote and that revision never had would fail here,
# loudly, rather than rehearsing a database no installation holds.
if [ -n "$populated" ]; then
	psql_as "$run" -f "$populated" >/dev/null
	note "populated $populated into $run"
fi

# The ledger the candidate inherits, read before it connects. The number that says whether this
# step measured anything at all (`N file(s) applied`) sits in the same report as the legitimate
# no-op, and the copy's own ledger is what tells the two releases apart: nothing pending because
# this release changes no schema, and nothing pending because the base already holds this tree's
# bytes, which is what a wrong `--base-ref` looks like. A copy restored from a database nothing
# migrated has no ledger to read, which is said rather than guessed at.
if ! ledger=$(psql_as "$run" -c "SELECT coalesce(count(*), 0) || ' applied version(s) for ' || count(distinct owner) || ' owner(s), highest version ' || coalesce(max(version)::text, 'none') FROM schema_migrations" 2>"$work/ledger.err"); then
	note "no base ledger to report: $(head -1 "$work/ledger.err")"
	ledger=""
else
	note "ledger the copy starts from: $ledger"
fi

# What the copy holds before the candidate connects is the evidence the upgrade is
# measured against, so it is written down here, once, and never retaken. Two facts:
# the applied ledger row by row, and one digest of the rows the seed wrote. The
# comparison happens after the candidate has run; a step that re-took its own "before"
# afterwards could only ever compare a database with itself.
#
# The data digest is scoped to the seeded rows by the marker the seed itself writes
# (`rehearse…@rehearse.localhost`, `task.created`), so a target that legitimately adds
# rows of its own is not reported as having lost data, while a changed body, a changed
# email or a dropped row changes the number. Rows the seed did not write are not this
# assertion's, and no target is asked to leave them alone.
ledger_snapshot() { # $1 = database, $2 = file
	psql_as "$1" -F $'\t' -c "SELECT owner || '/' || version, name, checksum, coalesce(applied_at::text,'') FROM schema_migrations ORDER BY 1" >"$2"
}
# The facts scripts/rehearse_facts.sh answers for the copy, taken once, here. It reads
# the per-tenant row digests, the pending outbox, the RLS state and — through the
# application role — what each tenant's own session may see of every other tenant's
# rows. Every one of those numbers has to be the same number after the candidate wrote.
facts() { # $1 = file
	"$root/scripts/rehearse_facts.sh" --admin-url "$admin_url" --database "$run" \
		--app-url "${PLATFORMKIT_TEST_DATABASE_URL:-}" >"$1" 2>"$work/facts.err"
}
before_ledger="$work/before-ledger.tsv"
before_facts="$work/before-facts.tsv"
unreadable_facts=0
# Counted once, on each side of the candidate, so that the receipt can name the figures the
# run actually measured: what the installation arrived with, what it left with, and how many
# of the arrived rows are still there row for row.
before_ledger_rows=0
applied_retained=0
if ledger_snapshot "$run" "$before_ledger" 2>"$work/snapshot.err"; then
	before_ledger_rows=$(grep -c '' <"$before_ledger")
	note "retained before the candidate connects: $before_ledger_rows ledger row(s)"
else
	note "nothing retained before the candidate: $(head -1 "$work/snapshot.err") — the copy holds no ledger to compare against"
fi
if facts "$before_facts"; then
	note "facts retained before the candidate: $(grep -c '' <"$before_facts") line(s), $(grep -c '^other/' <"$before_facts") of them tenant-boundary reads"
else
	# Not yet a finding about the release: a copy this step cannot read is a rehearsal that
	# did not happen, which is exit 2 and never a pass. The sentence that says why is
	# printed now, because it is the one an operator has to act on.
	echo "FACTS UNREADABLE before the candidate ran: $(head -1 "$work/facts.err")"
	unreadable_facts=1
fi

# The watcher: one session, one query, sampled every SAMPLE_MS milliseconds, and
# only this run's session carries the application name it filters on — which is also
# why the query excludes its own pid: the watcher's session carries that name as
# well, and a sample of itself waiting for something is not this run's lock wait.
#
# The query and the \watch that repeats it are written into one file which one psql
# reads. Measured reason, on the client this step runs on (psql 18.6): a
# `psql -c <query> -c '\watch 0.1'` answers the query once and then answers
# "\watch cannot be used with an empty query" — a query handed to -c is gone from the
# query buffer by the time the next -c runs, and \watch repeats the buffer. The
# sample file then holds one line however long the run took, every number derived
# from it is ~0ms, and the lock-wait finding can never fire. Read from a file the
# buffer does hold the query: the same `\watch 0.1` put sixteen samples into three
# seconds. The floor below is what turns that from a story into something the step
# checks on every run.
run_url="$(with_application_name "$(with_database "$admin_url" "$run")")"
watch_sql="$work/watch.sql"
printf '%s\n\\watch %s\n' \
	"SELECT count(*) FROM pg_stat_activity WHERE application_name = '$APPNAME' AND pid <> pg_backend_pid() AND wait_event_type = 'Lock'" \
	"$SAMPLE_S" >"$watch_sql"
watch_started=$(now_ms)
psql "$run_url" -At -o "$waits" -f "$watch_sql" >/dev/null 2>&1 &
watcher=$!

# The declaration the candidate boots with — the hosts it serves and the key its
# composition needs — is data, so it sits beside the fixtures the copy is seeded from
# rather than inside the run: scripts/testdata/rehearse/candidate.yaml.
write_config "$work/run.yaml" "${app_url:-$run_url}" "$run_url" "$(cat "$declaration")"
set +e
"$work/platformkit" migrate --drain --config "$work/run.yaml" >"$work/run.log" 2>&1
code=$?
set -e
kill "$watcher" 2>/dev/null || true
wait "$watcher" 2>/dev/null || true
watcher=""
watched_ms=$(( $(now_ms) - watch_started ))

# The candidate's own words, first, and before anything this step asks the database.
# Measured reason: under `set -Eeuo pipefail` a command substitution that fails ends
# the script where it stands, and a candidate that refused before it connecting left a
# copy migrated by the *previous* release's runner — which has no
# schema_migration_backfill to inspect — so this step's own unfinished-drain query was
# the last thing it printed and the rule's message, the one sentence the operator has
# to act on, was never printed at all.
if [ "$code" -ne 0 ]; then
	tail -20 "$work/run.log" >&2
	note "the migration failed; nothing past it applied"
fi

# Durations come from the runner's own lines, not from an estimate around the
# process: the runner timed the file, and nothing outside the transaction knows
# where a file began.
jq -R -r 'fromjson? // empty
	| select(.msg == "db: applied migration" or .msg == "db: drained data migration")
	| [.owner, (.version|tostring), .name, (.phase // "drain"), (.duration_ms|tostring)] | @tsv' \
	"$work/run.log" >"$work/files.tsv"

findings=$unreadable_facts
if [ "$unreadable_facts" -gt 0 ] && [ "$code" -eq 0 ]; then code=2; fi
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
lines=$(grep -c '' <"$waits" 2>/dev/null || true)
lock_ms=$((samples * SAMPLE_MS))

# Whether this step measured anything at all. Every interval of the window the watcher
# was alive for puts a line in the sample file, whether or not the count on it was
# non-zero, so a watcher that died at once — or a \watch that never repeated its query —
# leaves one line for a run of any length. Half the samples that window resolves to,
# after the watcher's own startup, is the floor: under it, "0 sample(s) ~ 0ms" would be
# a number the step never took, and the honest answer to a release is that the rehearsal
# could not run (exit 2) rather than a pass built on a measurement that did not happen.
# The window is the watcher's, not the candidate's wall clock: see now_ms.
expected=$(watch_floor "$watched_ms")
if [ "$lines" -lt "$expected" ]; then
	echo "LOCK WATCH BROKEN ${lines} sample line(s) in ${watched_ms}ms of watching; ${expected} is the fewest ${SAMPLE_MS}ms samples that window could hold"
	findings=$((findings + 1))
	[ "$code" -ne 0 ] || code=2
fi
if [ "$lock_ms" -gt "$max_lock" ]; then
	echo "LOCK WAIT ${lock_ms}ms > ${max_lock}ms"
	findings=$((findings + 1))
fi

# The bracket expression this line used to carry, `[^\n]*`, is not "not a newline" in a
# POSIX bracket: it is "not a backslash and not the letter n", so the pattern could not
# span the n inside the very words it was written to find and the branch under it never
# ran — a contended release was reported as an ordinary failure (exit 1) by the one step
# whose point is telling those two apart. The greedy form matches, and
# scripts/check_architecture_test.sh runs it over the log line the runner really wrote.
contended=$(grep -o 'db: migrate: .*contended.*' "$work/run.log" | tail -1 || true)
if [ -n "$contended" ]; then
	echo "CONTENDED $contended"
	findings=$((findings + 1))
	code=3
fi

# The invariant the two runner tables hold: a completed drain leaves no progress
# row. Anything here is a backfill the release did not finish. The query has no answer
# when the candidate refused before it connected — the copy was migrated by the previous
# release's runner, which may not have this table at all — and that is not the step's
# last word: the candidate's message, printed above, is.
if ! unfinished=$(psql_as "$run" -c "SELECT coalesce(string_agg(owner || '/' || version, ', ' ORDER BY owner, version), '') FROM schema_migration_backfill" 2>"$work/unfinished.err"); then
	note "no unfinished-drain check: $(head -1 "$work/unfinished.err")"
	unfinished=""
fi
if [ -n "$unfinished" ]; then
	echo "DRAIN UNFINISHED $unfinished"
	findings=$((findings + 1))
fi

# The retained facts, read again now that the candidate has written. Every ledger row the
# copy started with must still be there with the same name, checksum and application time
# — an applied migration that changed its bytes, lost its row or restamped its time is the
# immutable refusal kit/db makes, and an upgrade that rewrote history is not an upgrade.
# The fixtures' rows must digest to what they digested before, which is the sentence "the
# data survived" turned into something a run can fail, and every tenant-boundary read must
# still see nothing of its neighbours. Both sides are files this step wrote itself: one
# before the candidate connected, one after it stopped.
if [ -s "$before_ledger" ]; then
	if ! ledger_snapshot "$run" "$work/after-ledger.tsv" 2>"$work/after.err"; then
		echo "LEDGER UNREADABLE after the candidate ran: $(head -1 "$work/after.err")"
		findings=$((findings + 1))
		[ "$code" -ne 0 ] || code=1
	else
		lost_rows_file="$work/lost-rows.tsv"
		comm -23 <(cut -f1,2,3,4 "$before_ledger" | sort) <(cut -f1,2,3,4 "$work/after-ledger.tsv" | sort) >"$lost_rows_file"
		lost=$(head -3 "$lost_rows_file")
		# Counted here, where the rows are being compared, so that the receipt can name the
		# figure this fails on rather than a count of the after ledger called "retained".
		applied_retained=$((before_ledger_rows - $(grep -c '' <"$lost_rows_file" || true)))
		if [ -n "$lost" ]; then
			echo "LEDGER LOST applied rows the copy started from: $(printf '%s' "$lost" | tr '\n' ' ')"
			findings=$((findings + 1))
			[ "$code" -ne 0 ] || code=1
		fi
	fi
fi
if [ -s "$before_facts" ]; then
	if ! facts "$work/after-facts.tsv"; then
		echo "FACTS UNREADABLE after the candidate ran: $(head -1 "$work/facts.err")"
		findings=$((findings + 1))
		[ "$code" -ne 0 ] || code=1
	else
		# A line that was there before and is not now, or says something else, is the finding.
		# Two lines are allowed to move. `ledger` grows because the release applied files — the
		# rows it must not lose are checked row by row above — and `rls` grows because a release
		# that adds a tenant table adds a forced policy. A number that falls is the finding, and
		# for the authority line that is the only direction worth reading.
		changed=$(awk -F'\t' '
			NR == FNR { before[$1] = $0; next }
			{ after[$1] = $0 }
			END {
				for (k in before) {
					if (!(k in after)) print "gone\t" before[k]
					else if (after[k] != before[k] && k != "rls" && k != "ledger") { print "was\t" before[k]; print "now\t" after[k] }
				}
				for (k in after) if (!(k in before)) print "added\t" after[k]
			}' "$before_facts" "$work/after-facts.tsv" | head -8)
		if [ -n "$changed" ]; then
			echo "FACTS CHANGED the copy started with, one line each (was before, now after):"
			printf '%s\n' "$changed" | sed 's/^/  /'
			findings=$((findings + 1))
			[ "$code" -ne 0 ] || code=1
		fi
		lost_rls=$(awk -F'\t' '$1 == "rls" { print $2; exit }' "$before_facts")
		now_rls=$(awk -F'\t' '$1 == "rls" { print $2; exit }' "$work/after-facts.tsv")
		if [ -n "$lost_rls" ] && [ "$now_rls" != "$lost_rls" ] && [ "$now_rls" -lt "$lost_rls" ]; then
			echo "AUTHORITY LOST $lost_rls tenant table(s) forced row-level security before the upgrade and $now_rls do after"
			findings=$((findings + 1))
			[ "$code" -ne 0 ] || code=1
		fi
		leaks=$(awk -F'\t' '$1 ~ /^other\//' "$work/after-facts.tsv" |
			awk -F'\t' '$3 != "0" { printf "%s sees %s row(s) of another tenant ", $1, $3; seen = 1 } END { if (seen) print "" }')
		if [ -n "$leaks" ]; then
			echo "TENANT LEAK after the upgrade: $leaks"
			findings=$((findings + 1))
			[ "$code" -ne 0 ] || code=1
		fi
		[ -n "$changed" ] || note "facts retained: $(grep -c '' <"$before_facts") line(s) unchanged, $(grep -c '^other/' <"$work/after-facts.tsv") boundary read(s) all seeing one tenant"
	fi
fi

applied=$(wc -l <"$work/files.tsv")
note "$applied file(s) applied in ${total}ms; longest $longest at ${longest_ms}ms"
if [ "$applied" -eq 0 ]; then
	# Said plainly, because the line above is the same shape a no-op and a rehearsal that
	# measured nothing both print: the ledger this copy started from is the difference.
	note "nothing was pending for this candidate, against a copy starting from: ${ledger:-no ledger}"
fi
note "lock waits: $samples sample(s) of ${SAMPLE_MS}ms ≈ ${lock_ms}ms (${watched_ms}ms watched, sampled, so a wait shorter than ${SAMPLE_MS}ms can be missed)"
if [ "$code" -ne 0 ]; then
	# The candidate's message and the note that says what it stopped were printed
	# above, as soon as the run ended. What is left to decide here is which of the
	# four exit codes the release pipeline reads: a failure with no finding at all is
	# the plain failure (1), and the findings already named their own code (2 for a
	# step that could not measure, 3 for a budget overrun or a contended file).
	[ "$findings" -gt 0 ] || code=1
fi
if [ "$findings" -gt 0 ] && [ "$code" -eq 0 ]; then code=3; fi
if [ "$code" -eq 0 ]; then
	note "ok: ${applied} file(s) within ${max_file}s each and ${max_lock}ms of sampled lock waits, against a copy of ${base:-the dump}"
else
	note "failed: $findings finding(s); exit $code"
fi

# count_of runs one counting command and answers a number for it: a receipt field that is
# not a number refuses the write of the whole receipt, and the difference between "no facts
# were taken" and "the count could not be taken" is not worth a broken artefact.
count_of() {
	local value
	value=$("$@" 2>/dev/null) || value=""
	[[ "$value" =~ ^[0-9]+$ ]] || value=0
	printf '%s' "$value"
}

# The receipt. A consumer that pins this module owes the rehearsal — its own
# migrations run against its own data on this module's upgrade — and the one
# fact it can hand its CI is that *this revision* was rehearsed. So the step
# writes that fact where its caller can collect it, and writes it only on the
# path where it is true. REHEARSE_RECEIPT is unset for a local run and an
# artifact path in CI; scripts/check_pin_rehearsal.sh is the consumer's side of
# the bargain and scripts/PIN-REHEARSAL.md is the document. No lock wait is
# claimed per file, because none was measured per file: the watcher samples the
# run, so max_lock_ms is the run's and it is named once.
if [ "$code" -eq 0 ] && [ -n "${REHEARSE_RECEIPT:-}" ]; then
	files=$(jq -R -r 'fromjson? // empty
		| select(.msg == "db: applied migration" or .msg == "db: drained data migration")
		| {owner: .owner, version: (.version | tostring), name: .name,
		   phase: (.phase // "drain"), seconds: (.duration_ms / 1000)}' \
		"$work/run.log" | jq -s .)
	# The fixtures this case actually seeded, by digest, and the data half of what it
	# measured. The support record owes the same fixture bytes (scripts/supported_sources.sh
	# checks them against this tree every time it is read) and
	# scripts/check_pin_rehearsal.sh --strict refuses a case whose seeded files or retained
	# facts are not the ones the record and this run say: a fixture replaced by whatever SQL
	# the current tree holds rehearses a database no installation has.
	fixtures_json=$(printf '%s\n' "$seed" "$populated" |
		while IFS= read -r f; do
			[ -n "$f" ] && [ -r "$f" ] || continue
			printf '%s\t%s\n' "${f#"$root"/}" "$(sha256sum "$f" | cut -d' ' -f1)"
		done |
		jq -R -s 'split("\n") | map(select(length > 0) | split("\t") | {path: .[0], sha256: .[1]})')
	[ "$(jq -r 'length' <<<"$fixtures_json")" -gt 0 ] ||
		die 2 "this case seeded no fixture it could name: a receipt with no fixture digest cannot be checked against the support record"
	data_json=$(jq -n \
		--argjson lines "$(count_of grep -c '' "$before_facts")" \
		--argjson reads "$(count_of grep -c '^other/' "$work/after-facts.tsv")" \
		--argjson foreign "$(count_of awk -F'\t' '$1 ~ /^other\// { s += $3 } END { print s + 0 }' "$work/after-facts.tsv")" \
		--argjson before "$before_ledger_rows" \
		--argjson after "$(count_of grep -c '' "$work/after-ledger.tsv")" \
		--argjson retained "$applied_retained" \
		--argjson pending "$(count_of awk -F'\t' '$1 == "outbox.pending" { print $2; exit }' "$work/after-facts.tsv")" \
		'{facts_lines: $lines, boundary_reads: $reads, foreign_rows_visible: $foreign,
		  applied_rows_before: $before, applied_rows_after: $after, applied_rows_retained: $retained,
		  pending_outbox_rows: $pending}')
	# The bytes those figures came from, kept where a release reviewer can read them. The
	# run's temp directory goes when this process does, so the four files the ledger and fact
	# comparisons were made from are copied beside the receipt, and
	# scripts/check_pin_rehearsal.sh --strict re-derives every figure in `data` from these
	# files rather than trusting the run that wanted to pass. REHEARSE_EVIDENCE names where
	# they go — scripts/rehearse_supported_matrix.sh gives every case its own directory under
	# the aggregate receipt, so a case's evidence outlives the matrix's own temp directory —
	# and the default is a directory beside this case's receipt.
	evidence_dir="${REHEARSE_EVIDENCE:-${REHEARSE_RECEIPT}.d}"
	case "$evidence_dir" in /*) ;; *) evidence_dir="$PWD/$evidence_dir" ;; esac
	mkdir -p "$evidence_dir" || die 2 "cannot write the evidence directory $evidence_dir for $REHEARSE_RECEIPT"
	evidence_json='{}'
	for kept in "before_ledger:$before_ledger" "after_ledger:$work/after-ledger.tsv" \
		"before_facts:$before_facts" "after_facts:$work/after-facts.tsv"; do
		evidence_key="${kept%%:*}"
		evidence_src="${kept#*:}"
		[ -s "$evidence_src" ] ||
			die 2 "the receipt would name $evidence_key with no bytes behind it ($evidence_src): a case that could not take its own before-and-after evidence writes no receipt"
		cp "$evidence_src" "$evidence_dir/$evidence_key.tsv" ||
			die 2 "the evidence for $evidence_key could not be copied into $evidence_dir"
		evidence_json=$(jq -c --arg key "$evidence_key" --arg path "$evidence_dir/$evidence_key.tsv" \
			'. + {($key): $path}' <<<"$evidence_json") ||
			die 2 "the evidence path for $evidence_key could not be recorded"
	done
	note "evidence: $evidence_dir — the ledger and the facts, before and after, re-read by the release check"
	jq -n --arg base_ref "$ref" \
		--arg base_commit "$(git rev-parse --verify --quiet "${ref}^{commit}" || true)" \
		--arg candidate_commit "$(git rev-parse HEAD)" \
		--arg seed "$seed" \
		--argjson fixtures "$fixtures_json" \
		--argjson data "$data_json" \
		--argjson evidence "$evidence_json" \
		--argjson files "$files" \
		--argjson max_lock_ms "$lock_ms" \
		--argjson samples "$samples" \
		'{base_ref: (if $base_ref == "" then null else $base_ref end),
		  base_commit: (if $base_commit == "" then null else $base_commit end),
		  candidate_commit: $candidate_commit, seed: $seed, fixtures: $fixtures,
		  data: $data, evidence: $evidence, files: $files,
		  max_lock_ms: $max_lock_ms, lock_samples: $samples,
		  tool: "scripts/rehearse_migrations.sh", exit: 0}' >"$REHEARSE_RECEIPT" ||
		die 2 "the receipt could not be written to $REHEARSE_RECEIPT"
	note "receipt: $REHEARSE_RECEIPT for $(git rev-parse --short HEAD)"
fi
exit "$code"
