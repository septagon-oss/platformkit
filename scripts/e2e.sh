#!/usr/bin/env bash
# Gate 10: the admin shell renders and a generated CRUD screen works.
#
# It boots the application the way a person would and then drives it with a
# browser: a database of its own, migrated from nothing; one tenant and one
# administrator, created by `platformkit bootstrap`; the binary on a port; one
# Playwright spec; and then the application fixture is removed again. Failed
# browser results survive separately so a retry is not needed to inspect them.
#
# It is a script rather than four lines in the Makefile because the teardown has
# to happen whichever step failed, and a recipe cannot trap.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

# A port the caller named is theirs and this run uses exactly it. A run that names
# none takes one of its own from the kernel rather than a number the whole host
# reaches for: see free_port below, where the reason is spelled out.
port="${PLATFORMKIT_E2E_PORT:-}"
admin_url="${PLATFORMKIT_TEST_ADMIN_URL:?the owner connection; make e2e exports it}"
app_url="${PLATFORMKIT_TEST_DATABASE_URL:?the application connection; make e2e exports it}"
database="platformkit_e2e_$(date +%s)_${RANDOM}_$$"

if ! command -v node >/dev/null; then
	echo "e2e: node is not installed; gate 10 needs it. See e2e/package.json." >&2
	exit 1
fi

# A URL with the database swapped for this run's own. Everything else — host,
# port, credentials — is whatever the suite already uses.
swap() {
	node -e 'try {
		const url = new URL(process.argv[1]);
		if (!["postgres:", "postgresql:"].includes(url.protocol)) throw new Error();
		url.pathname = "/" + process.argv[2];
		process.stdout.write(url.toString());
	} catch { console.error("e2e: invalid PostgreSQL URL"); process.exit(1); }' "$1" "${2:-$database}"
}
psql_admin() { psql "$(swap "$admin_url" postgres)" -v ON_ERROR_STOP=1 -q "$@"; }

work="$(mktemp -d)"
results=""
app_pid=""
created=false
# Whether a failed attempt already printed the application's own log, so the report at
# the end of the run says each thing once.
logged=false
cleanup() {
	status=$?
	if [ -n "$app_pid" ]; then
		kill "$app_pid" 2>/dev/null || true
		wait "$app_pid" 2>/dev/null || true
	fi
	if "$created"; then
		psql_admin -c "DROP DATABASE $database WITH (FORCE);" >/dev/null || echo "e2e: could not remove $database" >&2
	fi
	rm -rf "$work"
	if [ -n "$results" ]; then
		if [ "$status" -eq 0 ]; then
			rm -rf "$results"
		else
			echo "e2e: Playwright failure results retained at $results" >&2
		fi
	fi
	return "$status"
}
trap cleanup EXIT

# The binary is built rather than `go run`: go run execs the compiled program as
# a child, so killing it at the end of this script would leave the application
# holding the port and the next run would drive the previous run's build.
go build -o "$work/platformkit" ./apps/platformkit

# Exercise the real Storybook build behind the same authenticated application.
[ -d ui/storybook/node_modules ] || npm --prefix ui/storybook ci --no-audit --no-fund
go run ./tools/designexport >"$work/storybook.json"
if ! npm --prefix ui/storybook run build -- "$work/storybook" <"$work/storybook.json" >"$work/storybook.log" 2>&1; then
	cat "$work/storybook.log" >&2
	exit 1
fi

echo "e2e: a database of its own"
psql_admin -c "CREATE DATABASE $database;" >/dev/null
created=true
# The role exists (apps/platformkit/postgres-init.sql made it); the grants are per
# database, so a fresh one needs them again.
psql "$(swap "$admin_url")" -v ON_ERROR_STOP=1 -q \
	-c "GRANT USAGE ON SCHEMA public TO platformkit_app;" \
	-c "ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO platformkit_app;" \
	-c "ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO platformkit_app;"

# The configuration names the address this run serves on, so it is written by a
# function and not once: an attempt that loses the port writes it again for the
# next one. Everything below the address is the same for every attempt — the same
# database, the same Storybook build, the same byte store.
write_config() {
	cat >"$work/config.yaml" <<YAML
server:
  addr: "127.0.0.1:$port"
  public_host: "localhost:$port"
  docs: false
  storybook_dir: "$work/storybook"
database:
  url: "$(swap "$app_url")"
  migrate_url: "$(swap "$admin_url")"
nats:
  url: "${PLATFORMKIT_TEST_NATS_URL:-nats://localhost:4222}"
log:
  level: "warn"
audit:
  retention_days: 365
files:
  dir: "$work/files"
YAML
}

password="e2e-$(date +%s)-password"
# Runtime overrides belong to the caller's application, not this fixture, so
# the function execs after clearing them, and is therefore only ever called in
# a subshell: the explicit one around bootstrap, and the fork `&` makes for the
# server. Were the body itself a subshell, bash would put a shell of its own
# between this script and the backgrounded application; cleanup would stop that
# shell and orphan the application, still on the port, for the next run to hit.
run_app() {
	for variable in "${!PLATFORMKIT_@}"; do unset "$variable"; done
	export PLATFORMKIT_BOOTSTRAP_PASSWORD="$password"
	exec "$work/platformkit" "$@"
}
# An unnamed port comes from the kernel, which hands out one nobody is holding.
# The reason is the one this gate has been losing runs to: with 8099 as the
# default, every worktree on one host asks for the same address, and a run that
# loses the race does not notice — the readiness wait below used to break out of
# itself on any answer to GET /health, and kit/health answers liveness with the
# constant {"status":"ok"} and no Server header, so a neighbour's application
# satisfies it exactly. The run then drives that neighbour's app with this run's
# credentials, and when the neighbour finishes, every remaining case sees
# net::ERR_CONNECTION_REFUSED. Nothing in the product was involved in either.
free_port() {
	node -e 'const server = require("node:net").createServer();
		server.listen(0, "127.0.0.1", () => {
			const chosen = server.address().port;
			server.close(() => console.log(chosen));
		});'
}
[ -n "$port" ] || port="$(free_port)"
# A node that answers with nothing would leave the address ending in a bare colon,
# the application choosing its own port behind this script's back, and every case
# below driving an address nobody wrote down.
case "$port" in
	''|*[!0-9]*)
		echo "e2e: no port to serve on: \"$port\" is not one." >&2
		exit 1
		;;
esac
write_config

echo "e2e: one tenant and one administrator"
# --language is the tenant's own declaration, not the deployment's: e2e/localization.spec.ts
# drives a Portuguese browser and expects the sign-in page in Portuguese, and the only thing
# that makes a tenant served in a second language is somebody saying so. A tenant created
# without it is served in the one language the copy is written in.
(run_app bootstrap --config "$work/config.yaml" \
	--tenant e2e --host localhost --name "End to end" --admin-email admin@e2e.test \
	--language pt-PT) >/dev/null

# Is the process this script started the one answering on $port? Where ss can name
# the owner of a socket — it does for a socket of this user, without privileges —
# the owner has to be this script's own pid, and only then is GET /health asked,
# which says the application serves rather than that it bound.
serves_on_port() {
	if command -v ss >/dev/null; then
		local rows
		# The header line above ss's rows carries no "pid=", so only the rows are read, and
		# no -H is needed to hide it. `grep -q` is not used either: this script runs under
		# pipefail, and a grep that answers before ss has written everything kills ss with
		# SIGPIPE, whose 141 the pipeline reports as "no listener" — a listener present read
		# as one absent. scripts/rehearse_migrations.sh:336 carries the same lesson.
		rows="$(ss -ltnp "sport = :$port" 2>/dev/null | tail -n +2)"
		case "$rows" in
			# This run's own listener: go on and ask it.
			*"pid=$app_pid,"*) ;;
			# A listener somebody else holds, and ss has named them: nothing to ask.
			*'users:('*) return 1 ;;
			# Nothing is listening yet.
			'') return 1 ;;
			# A listener whose owner ss will not name. Such a host gives no process
			# information at all, and refusing on it would stop the gate booting on a
			# machine that has done nothing wrong: there the probe decides, as it does
			# where ss is not installed.
			*) ;;
		esac
	fi
	curl -fsS "http://localhost:$port/health" >/dev/null 2>&1
}

# Boot the application and wait for it to be serving, once. Two attempts, because a
# port chosen from the kernel is released again the moment it is chosen — the
# application migrates before it listens, so somebody else can take the number
# inside that window. When the caller named the port there is nothing to retry:
# the name they gave is the address the run uses, and a port that is not free is
# their own to free.
boot_and_wait() {
	local attempt held_port lost
	for attempt in 1 2; do
		if [ "$attempt" != 1 ]; then
			# A name the caller gave is the address the run uses; a port that is not free
			# under that name is theirs to free, not this script's to route around.
			if [ -n "${PLATFORMKIT_E2E_PORT:-}" ]; then return 1; fi
			lost="$port"
			port="$(free_port)"
			write_config
			echo "e2e: $lost was taken before the application bound it; serving on $port" >&2
		elif [ -n "${PLATFORMKIT_E2E_PORT:-}" ] && command -v ss >/dev/null \
				&& ss -ltn 2>/dev/null | grep ":$port " >/dev/null; then
			# Only a port the caller named is refused here, and quickly: they asked for this
			# address, so the answer they need is that it is taken. A port this run chose for
			# itself is not a promise, and saying "set PLATFORMKIT_E2E_PORT" about a number
			# nobody set would be advice nobody can act on — that attempt simply fails, and
			# the next one takes a fresh port.
			echo "e2e: something is already listening on $port; stop it or name another port with PLATFORMKIT_E2E_PORT." >&2
			return 1
		fi
		echo "e2e: serving on $port"
		run_app run --config "$work/config.yaml" >"$work/app.log" 2>&1 &
		app_pid=$!
		held_port=false
		for _ in $(seq 1 60); do
			# The process first, the answer second: an answer from a stranger must not
			# hide that this run's own application has already stopped.
			if ! kill -0 "$app_pid" 2>/dev/null; then
				echo "e2e: the application stopped before it served:" >&2
				cat "$work/app.log" >&2
				logged=true
				break
			fi
			if serves_on_port; then held_port=true; break; fi
			sleep 1
		done
		if [ "$held_port" = true ]; then return 0; fi
		# This attempt's application is not serving. Stop it before the next one, so a
		# retry never leaves a second process alive on the address or on the database.
		kill "$app_pid" 2>/dev/null || true
		wait "$app_pid" 2>/dev/null || true
		app_pid=""
	done
	return 1
}

if ! boot_and_wait; then
	echo "e2e: the application this run started never served on $port." >&2
	if command -v ss >/dev/null; then
		# Name whoever does hold it: that is the fact a reader needs, and the only
		# reason this gate ever failed before was that nobody could say it.
		ss -ltnp "sport = :$port" 2>&1 | sed 's|^|e2e:   |' >&2
	fi
	if [ "$logged" = false ] && [ -f "$work/app.log" ]; then cat "$work/app.log" >&2; fi
	exit 1
fi

cd e2e
# npm ci and not npm install: ci installs exactly what package-lock.json pins
# and fails when the lock and the manifest disagree, which is what a gate wants.
# install resolves afresh and rewrites the lock, so gate 10 could pass against a
# Playwright nobody chose and leave the lock changed in somebody's working tree.
#
# The condition is the installed tree and not the directory's existence. npm
# records what it installed in node_modules/.package-lock.json; an install made
# before a dependency change still leaves a node_modules behind, so a lock that
# raises Playwright, its Chromium or axe-core would go on being ignored on every
# developer machine while CI, which always installs from nothing, ran the chosen
# versions. A gate that only the runner really runs is not a gate.
matches_lock() {
	node -e 'const fs = require("node:fs");
		const packages = path => JSON.parse(fs.readFileSync(path, "utf8")).packages ?? {};
		let installed;
		try { installed = packages("node_modules/.package-lock.json"); } catch { process.exit(1); }
		for (const [name, pinned] of Object.entries(packages("package-lock.json"))) {
			// Optional dependencies are platform-specific: npm is right not to
			// install a darwin watcher here, and absence is not drift.
			if (!name || pinned.optional) continue;
			if (installed[name]?.version !== pinned.version) {
				console.error(`e2e: ${name} is ${installed[name]?.version ?? "not installed"}, package-lock.json pins ${pinned.version}; reinstalling`);
				process.exit(1);
			}
		}'
}
matches_lock || npm ci --no-audit --no-fund
headless=true
default_output=true
for argument in "$@"; do
	case "$argument" in
		--headed|--debug|--ui|--ui-*) headless=false ;;
		--output|--output=*) default_output=false ;;
	esac
done
# A forwarded display can stop headless Chromium animation frames. Preserve
# it for interactive runs, including Playwright's environment-based debugger.
if "$headless" && [ "${PWDEBUG:-0}" = 0 ]; then unset DISPLAY; fi
output_args=()
if "$default_output"; then
	results="$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/platformkit-e2e-results.XXXXXX")"
	output_args=(--output "$results")
fi
PLATFORMKIT_E2E_URL="http://localhost:$port" \
	PLATFORMKIT_E2E_PORT="$port" \
	PLATFORMKIT_E2E_FIXTURE_DATABASE="$database" \
	PLATFORMKIT_E2E_EMAIL="admin@e2e.test" \
	PLATFORMKIT_E2E_PASSWORD="$password" \
	npx playwright test "${output_args[@]}" "$@"
