#!/usr/bin/env bash
# Measure the public first use of this application on a machine with nothing but
# a Go toolchain, and write one receipt line per sample.
#
# The quantities are the ones an installation promise is made of, each including
# the part that is not this program's fault:
#
#   fetch_ms        go install …@main in a clean home — module download, toolchain
#                   download when the home's Go is older than the toolchain line,
#                   and the compile
#   cold_ms         the first `start` in that home, until /health answers 200:
#                   the database download, its extract, initdb, migrations, bootstrap
#   first_action_ms sign in, create a task, read it back
#   warm_ms         the same command again over the same data directory, until
#                   /health answers 200, with the task still readable afterwards
#   attempts[]      every failed attempt beside them, timed: a refusal that costs
#                   a person two minutes is part of the two minutes they spent
#
# Each sample gets its own HOME, so each one pays for the module, the toolchain,
# the compile and the database download from nothing; the home is removed once the
# sample is in. Ports are taken from the operating system and every process this
# script starts is stopped by the pid it recorded.
#
# What is written is one JSON object per line, and only for a sample that reached a
# persisted task: a sample that refused, was interrupted, or could not read its own
# timings writes nothing, so a receipt cannot describe a run that did not happen.
# Nothing this script keeps holds a credential: every capture of a run goes through
# scripts/redact_credentials.sh, the first run's transcript only after the
# administrator's password has been read out of the pipe into a shell variable and
# the raw bytes unlinked. Files are written 0600 and the scratch tree is the
# sample's own; an interrupt runs the same cleanup a normal exit runs.
#
# The artifact measured is the one in this checkout, installed into a home that has
# never seen its dependencies: that is the build a stranger gets one step later,
# through the proxy, and the receipt names the revision it came from beside the
# version the public `@main` command resolves to. With --published the install goes
# through the proxy at @main instead, which is the command the README prints.
#
# Usage: first_use.sh [--samples N] [--published] [--out FILE]
set -euo pipefail
# The samples capture the terminal output of a run that prints a password, so
# nothing this script writes is readable by anybody else, and the scratch tree is
# created private.
umask 077

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$here/.." && pwd)"
module="${FIRST_USE_MODULE:-github.com/septagon-oss/platformkit/apps/platformkit}"
published=no
samples=5
out="${FIRST_USE_OUT:-first-use.jsonl}"
scratch="${TMPDIR:-/tmp}/platformkit-first-use-$$"

while [ $# -gt 0 ]; do
	case "$1" in
	--samples) samples="$2"; shift 2 ;;
	--published) published=yes; shift ;;
	--out) out="$2"; shift 2 ;;
	*) printf 'first_use.sh: %s is not an option\n' "$1" >&2; exit 2 ;;
	esac
done

command -v go >/dev/null || { echo 'first_use.sh: no go on PATH, which is the one prerequisite' >&2; exit 2; }
mkdir -p "$scratch"
trap 'discard_scratch' EXIT
# An interrupt or a termination runs the same cleanup as a normal exit: a kept
# capture of a first run holds the administrator's password until the redactor has
# had it, and a kill that skipped the cleanup would keep it longer than this file
# means to.
trap 'exit 130' INT
trap 'exit 143' TERM

# discard_scratch removes the run's scratch tree, opening the read-only module
# caches the samples left in it.
discard_scratch() {
	chmod -R u+w "$scratch" 2>/dev/null
	rm -rf "$scratch" 2>/dev/null
}

now_ms() { date +%s%3N; }
elapsed() { printf '%s' $(($(now_ms) - $1)); }

# free_port is a port the kernel says is free right now, on the loopback only.
free_port() {
	python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1]);s.close()'
}

# hold_port binds one port and does not answer until the bind exists, so the
# occupied-address attempt is measured against a listener this script started and
# stops by pid. Without the readiness wait the attempt could bind the port first and
# be a normal successful start with a name that meant the opposite.
hold_port() {
	local pid marker
	marker="$scratch/holder.$1"
	python3 - "$1" <<PYHOLD >"$marker" &
import socket, sys, time
s = socket.socket()
s.bind(("127.0.0.1", int(sys.argv[1])))
s.listen(1)
sys.stdout.write("ready\n")
sys.stdout.flush()
time.sleep(120)
PYHOLD
	pid=$!
	for _ in $(seq 1 100); do
		if grep -q ready "$marker"; then break; fi
		sleep 0.1
	done
	printf '%s' "$pid"
}

# port_is_answered says whether a listener holds one port, which is how a refusal is
# attributed to the port rather than to the harness being quick.
port_is_answered() {
	python3 -c 'import socket,sys
try:
    socket.create_connection(("127.0.0.1", int(sys.argv[1])), 2).close()
except OSError:
    sys.exit(1)' "$1"
}

# wait_health asks the running reference until it answers, and says which port it
# gave up on when it did not.
wait_health() {
	local url="$1" limit="$2" start
	start="$(now_ms)"
	while [ "$(elapsed "$start")" -lt "$limit" ]; do
		if [ "$(curl -s -o /dev/null -w '%{http_code}' "$url" 2>/dev/null)" = "200" ]; then
			return 0
		fi
		sleep 0.2
	done
	return 1
}

# run_redacted keeps a command's combined output with the credentials rewritten.
run_redacted() {
	local destination="$1"
	shift
	"$@" 2>&1 | bash "$here/redact_credentials.sh" >"$destination"
}

# redact_kept rewrites a raw capture through the redactor, keeps the redacted file
# and unlinks the raw one. It is called as soon as a sample has read what it needed
# from the raw bytes, so a sample that fails after this point leaves a redacted
# transcript behind and never the credential: the first run of `start` prints the
# administrator's password and stores nothing, so the transcript is the credential.
# The path of the kept file is printed for the message that names it.
redact_kept() {
	local raw="$1" kept="${1%.raw}.log"
	bash "$here/redact_credentials.sh" <"$raw" >"$kept" && rm -f "$raw"
	printf '%s' "$kept"
}

receipt_for_sample() {
	local home data bin port base start attempts
	home="$(mktemp -d "${scratch}/home.XXXXXX")"
	data="$(mktemp -d "${scratch}/data.XXXXXX")"
	port="$(free_port)"
	base="http://platformkit.localhost:${port}"

	# The fetch, in a home that has never seen this module's dependencies. GOPRIVATE
	# is cleared because a maintainer's own configuration sends the go command to git
	# rather than to the proxy, and a stranger has no such file.
	start="$(now_ms)"
	target="$root/apps/platformkit"
	[ "$published" = yes ] && target="${module}@main"
	if ! HOME="$home" GOPRIVATE= GOFLAGS= go install "$target" >"${home}/fetch.log" 2>&1; then
		printf 'fetch failed; see %s\n' "${home}/fetch.log" >&2
		return 1
	fi
	fetch_ms="$(elapsed "$start")"
	bin="${home}/go/bin/platformkit"
	[ -x "$bin" ] || { echo "the install left no binary at $bin" >&2; return 1; }

	# The failed attempts, before anything of this installation exists: each one
	# timed, because the time a refusal costs is part of what the person spent.
	attempts=""
	occupied="$(free_port)"
	holder="$(hold_port "$occupied")"
	if ! port_is_answered "$occupied"; then
		kill "$holder" 2>/dev/null || true
		echo "nothing answered $occupied, so an occupied address cannot be measured" >&2
		return 1
	fi
	start="$(now_ms)"
	refused=0
	run_redacted "${scratch}/occupied.log" env HOME="$home" timeout 180 "$bin" start \
		--addr "127.0.0.1:${occupied}" --data "${data}-occupied" || refused=$?
	kill "$holder" 2>/dev/null || true
	wait "$holder" 2>/dev/null || true
	if [ "$refused" -eq 0 ]; then
		echo "an occupied address was accepted" >&2
		return 1
	fi
	attempts="${attempts}{\"kind\":\"occupied_addr\",\"ms\":$(elapsed "$start"),\"exit\":$refused},"

	readonly_parent="$(mktemp -d "${scratch}/readonly.XXXXXX")"
	chmod 0500 "$readonly_parent"
	start="$(now_ms)"
	refused=0
	run_redacted "${scratch}/readonly.log" env HOME="$home" timeout 180 "$bin" start \
		--addr "127.0.0.1:${port}" --data "${readonly_parent}/data" || refused=$?
	chmod 0700 "$readonly_parent"
	if [ "$refused" -eq 0 ]; then
		echo "an unwritable --data was accepted" >&2
		return 1
	fi
	attempts="${attempts}{\"kind\":\"unwritable_data\",\"ms\":$(elapsed "$start"),\"exit\":$refused},"

	# The cold start: the first time this command has ever run in this home.
	start="$(now_ms)"
	HOME="$home" "$bin" start --addr "127.0.0.1:${port}" --data "$data" >"${home}/cold.raw" 2>&1 &
	cold_pid=$!
	if ! wait_health "${base}/health" 300000; then
		kill "$cold_pid" 2>/dev/null || true
		wait "$cold_pid" 2>/dev/null || true
		printf 'the cold start never answered; %s\n' "$(redact_kept "${home}/cold.raw")" >&2
		return 1
	fi
	cold_ms="$(elapsed "$start")"

	# The first useful action, through the public doors and no other: sign in, then
	# persist a task, then read it back. The password is read out of the raw capture
	# and the raw bytes go away with it: the server still holds its own descriptor,
	# so nothing further it prints reaches a kept file either.
	password="$(sed -n 's/^[[:space:]]*password:[[:space:]]*//p' "${home}/cold.raw" | head -1)"
	cold_log="$(redact_kept "${home}/cold.raw")"
	[ -n "$password" ] || { echo "the first run printed no password to sign in with; $cold_log" >&2; return 1; }
	start="$(now_ms)"
	if ! curl -s -o /dev/null -c "${home}/jar" -H 'content-type: application/json' \
		-d "{\"email\":\"admin@platformkit.localhost\",\"password\":\"${password}\"}" \
		"${base}/api/v1/auth/login" || [ ! -s "${home}/jar" ]; then
		echo "the first sign-in failed" >&2
		return 1
	fi
	created="$(curl -s -o /dev/null -w '%{http_code}' -b "${home}/jar" -H 'content-type: application/json' \
		-d '{"title":"first task"}' "${base}/api/v1/task/tasks")"
	listed="$(curl -s -b "${home}/jar" "${base}/api/v1/task/tasks")"
	case "$listed" in *"first task"*) persisted=yes ;; *) persisted=no ;; esac
	first_action_ms="$(elapsed "$start")"
	# The password is in no kept file and lives in this shell only until the sample
	# is in; the sign-in above is the one place it is used.
	password=""
	[ "$created" = 201 ] || { echo "creating the task answered $created" >&2; return 1; }
	[ "$persisted" = yes ] || { echo "the created task does not read back" >&2; return 1; }

	# The rerun: stopped by the pid this script recorded, then started again over
	# the same data directory. It must reuse the installation and print no password.
	kill "$cold_pid"
	wait "$cold_pid" 2>/dev/null || true
	start="$(now_ms)"
	HOME="$home" "$bin" start --addr "127.0.0.1:${port}" --data "$data" >"${home}/warm.raw" 2>&1 &
	warm_pid=$!
	if ! wait_health "${base}/health" 120000; then
		kill "$warm_pid" 2>/dev/null || true
		echo "the rerun never answered; $(redact_kept "${home}/warm.raw")" >&2
		return 1
	fi
	warm_ms="$(elapsed "$start")"
	rerun_task="$(curl -s -b "${home}/jar" "${base}/api/v1/task/tasks")"
	rerun_passwords="$(grep -c '^[[:space:]]*password:' "${home}/warm.raw" || true)"
	warm_log="$(redact_kept "${home}/warm.raw")"
	kill "$warm_pid"
	wait "$warm_pid" 2>/dev/null || true
	case "$rerun_task" in *"first task"*) ;; *) echo "the rerun lost the task; $warm_log" >&2; return 1 ;; esac
	[ "$rerun_passwords" = 0 ] || { echo "the rerun printed ${rerun_passwords} password line(s); $warm_log" >&2; return 1; }

	# What the run was, stated by the run: the revision in this checkout, the
	# version the public `@main` resolves to, and the toolchain the sample's own
	# home answered with.
	revision="$(git -C "$root" rev-parse HEAD)"
	# What `@main` resolved to belongs in the receipt: a claim about a moving branch
	# that names no version points at a commit nobody can go back to. The install
	# target is a package and the name the proxy answers is the module that contains
	# it, one path up — asking for the package path is refused as no module at all —
	# and GOPRIVATE is cleared again because this host sends its own organisations to
	# git while a stranger's answer comes from the proxy.
	proxy_module="${module%/apps/*}"
	pseudo="$(cd "$root" && HOME="$home" GOPRIVATE= go list -m -json "${proxy_module}@main" 2>/dev/null |
		sed -n 's/.*"Version": "\(.*\)".*/\1/p')"
	if [ "$published" = yes ] && [ -z "$pseudo" ]; then
		echo "the proxy answered no version for ${proxy_module}@main, so this receipt could not name what it measured" >&2
		return 1
	fi
	goversion="$(HOME="$home" go version | cut -d' ' -f3)"
	attempts="${attempts%,}"
	download_bytes="$(size_of "$home/.cache/platformkit/postgres/archive")"
	home_kb="$(du -sk "$home" | cut -f1)"
	cache_kb="$(du -sk "$home/.cache" | cut -f1)"
	data_kb="$(du -sk "$data" | cut -f1)"
	git_state=clean
	[ -z "$(git -C "$root" status --porcelain)" ] || git_state=dirty
	artifact=worktree
	[ "$published" = yes ] && artifact=published

	# The receipt is written from the environment rather than from interpolated
	# shell text: a line of JSON full of braces is exactly what a here-document
	# expansion can bend, and a receipt that failed to be written is worse than a
	# sample that never ran.
	FIRST_OUT="$out" FIRST_REVISION="$revision" FIRST_PUBLISH="$pseudo" FIRST_GO="$goversion" \
		FIRST_ARTIFACT="$artifact" FIRST_STATE="$git_state" FIRST_HOST="$(hostname)" \
		FIRST_GOOS="$(go env GOOS)" FIRST_GOARCH="$(go env GOARCH)" \
		FIRST_DOWNLOAD_BYTES="$download_bytes" FIRST_HOME_KB="$home_kb" FIRST_CACHE_KB="$cache_kb" \
		FIRST_DATA_KB="$data_kb" FIRST_FETCH_MS="$fetch_ms" FIRST_COLD_MS="$cold_ms" \
		FIRST_ACTION_MS="$first_action_ms" FIRST_WARM_MS="$warm_ms" FIRST_ATTEMPTS="$attempts" \
		python3 - <<'PY'
import json, os
def number(name):
	return int(os.environ[name])
record = {
	"tool": "scripts/first_use.sh",
	"revision": os.environ["FIRST_REVISION"],
	"published_version": os.environ["FIRST_PUBLISH"],
	"go": os.environ["FIRST_GO"],
	"goos": os.environ["FIRST_GOOS"],
	"goarch": os.environ["FIRST_GOARCH"],
	"host": os.environ["FIRST_HOST"],
	"artifact": os.environ["FIRST_ARTIFACT"],
	"worktree": os.environ["FIRST_STATE"],
	"download_bytes": number("FIRST_DOWNLOAD_BYTES"),
	"home_kb": number("FIRST_HOME_KB"),
	"cache_kb": number("FIRST_CACHE_KB"),
	"data_kb": number("FIRST_DATA_KB"),
	"fetch_ms": number("FIRST_FETCH_MS"),
	"cold_ms": number("FIRST_COLD_MS"),
	"first_action_ms": number("FIRST_ACTION_MS"),
	"warm_ms": number("FIRST_WARM_MS"),
	"cold_plus_first_ms": number("FIRST_FETCH_MS") + number("FIRST_COLD_MS") + number("FIRST_ACTION_MS"),
	"task_persisted": True,
	"rerun_reused": True,
	"attempts": json.loads("[" + os.environ["FIRST_ATTEMPTS"] + "]"),
}
with open(os.environ["FIRST_OUT"], "a") as handle:
	json.dump(record, handle, separators=(",", ":"))
	handle.write("\n")
PY
	discard_home "$home"
}

# discard_home removes a sample's home. The Go module cache is written read-only on
# purpose, so the trees have to be opened before they can be unlinked; a home left
# behind would be several gigabytes of evidence nobody asked to keep.
discard_home() {
	chmod -R u+w "$1" 2>/dev/null
	rm -rf "$1" 2>/dev/null
}

# size_of prints the bytes of the database archive the sample downloaded, or 0 when
# the cache held one already — a warm cache is not a download and the receipt says
# which it measured.
size_of() {
	local total=0 file
	for file in "$1"/*.txz "$1"/*.jar; do
		[ -f "$file" ] && total=$((total + $(stat -c%s "$file")))
	done
	printf '%s' "$total"
}

failures=0
for _ in $(seq 1 "$samples"); do
	if ! receipt_for_sample; then
		failures=$((failures + 1))
		echo "first_use.sh: a sample did not reach a persisted task, so it wrote no receipt" >&2
	fi
done

echo "first_use.sh: ${samples} sample(s) asked, ${failures} wrote no receipt; receipts are in ${out}"
[ "$failures" -eq 0 ]
