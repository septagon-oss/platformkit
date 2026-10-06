#!/usr/bin/env bash
# Gate 10: the admin shell renders and a generated CRUD screen works.
#
# It boots the application the way a person would and then drives it with a
# browser: a database of its own, migrated from nothing; one tenant and one
# administrator, created by `platformkit bootstrap`; the binary on a port; the
# Playwright specs; and then the application fixture is removed again. Mails go to
# compose.yaml's mailpit catcher, which is where the journeys that open a mailed
# link find it. Failed browser results survive separately so a retry is not needed
# to inspect them.
#
# It is a script rather than four lines in the Makefile because the teardown has
# to happen whichever step failed, and a recipe cannot trap.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

requested_port="${PLATFORMKIT_E2E_PORT:-}"
admin_url="${PLATFORMKIT_TEST_ADMIN_URL:?the owner connection; make e2e exports it}"
app_url="${PLATFORMKIT_TEST_DATABASE_URL:?the application connection; make e2e exports it}"
database="platformkit_e2e_$(date +%s)_${RANDOM}_$$"

if ! command -v node >/dev/null; then
	echo "e2e: node is not installed; gate 10 needs it. See e2e/package.json." >&2
	exit 1
fi

# shellcheck source=scripts/free_port.sh
. "$root/scripts/free_port.sh"

# The machine has to be able to say whose socket is whose before this run is
# allowed to trust a listener — see scripts/free_port.sh for why an answer on
# /health is not the application's signature. The question is answered here, at the
# same end as the node check, because a run that cannot prove what it serves is
# refused before it builds a binary, creates a database or bootstraps a tenant.
if ! port_attribution_works; then
	echo "e2e: this machine cannot say which process is listening on a port — a listener this run started itself went unattributed, so neither ss -p nor lsof is answering. Gate 10 will not hand a browser an address it cannot prove it serves." >&2
	exit 1
fi

# The port this run serves on, asked for rather than assumed — scripts/free_port.sh
# says why. A caller that names one gets exactly that port or nothing: the browser
# run at the end reads PLATFORMKIT_E2E_PORT as the address the harness is allowed to
# write through (e2e/session-recovery.spec.ts refuses every other), so serving
# elsewhere would have this run drive somebody else's application. A caller that
# names none takes what the kernel allocates, which is what lets two runs on one
# host go at the same time.
#
# This is settled before the database exists and before the binary is built: a run
# that cannot be given the port it was promised has refused before it touched
# anything. What it does not settle is who holds the port at the moment of the bind,
# which is the question serve() below asks once the application is up.
if [ -n "$requested_port" ]; then
	if ! bind_free_port "$requested_port" >/dev/null; then
		echo "e2e: something is already listening on $requested_port; set PLATFORMKIT_E2E_PORT to a port that is free, or unset it and let this run choose one." >&2
		exit 1
	fi
	port="$requested_port"
else
	if ! port="$(bind_free_port)"; then
		echo "e2e: no free loopback port to serve on." >&2
		exit 1
	fi
fi

# The development mail catcher: compose.yaml's mailpit service. The application is
# pointed at its SMTP port and the journeys are pointed at its HTTP API, because a
# mailed link nobody can read is the failure the front-door journeys exist for.
#
# The three names are derived here, from the two ports compose.yaml publishes, rather
# than by the `e2e` recipe. The reason is who else calls this script: `./scripts/e2e.sh
# some-spec.spec.ts` is how somebody runs one journey, and that caller carries the
# stack's ports in its environment but none of the recipe's derived names, so a
# recipe-side derivation left the direct call dialling the default 8025 while the
# catcher it was given answered on another port. A caller that reaches a catcher at some
# other address - a CI job whose services answer by container name, not on localhost -
# sets the three names itself, and wins over the derivation.
mail_host="${PLATFORMKIT_E2E_MAIL_HOST:-localhost}"
mail_port="${PLATFORMKIT_E2E_MAIL_PORT:-${PLATFORMKIT_MAILPIT_SMTP_PORT:-1025}}"
mailpit_url="${PLATFORMKIT_E2E_MAILPIT_URL:-http://${mail_host}:${PLATFORMKIT_MAILPIT_PORT:-8025}}"

# Asked for before the binary is built, not after the browser starts clicking. With
# no catcher the application boots happily and logs the messages it will not send
# (apps/platformkit's mailer wires the in-process mailbox when no host is set), so the
# journey that reads a mailed link fails on a 30-second poll against a URL nobody
# answered, pointing at Playwright rather than at the stack that is missing.
if ! curl -fsS --max-time 5 "$mailpit_url/api/v1/info" >/dev/null 2>&1; then
	echo "e2e: no Mailpit API at $mailpit_url, and the mailed-link journeys read the link a run sends out of it." >&2
	echo "     docker compose up -d --wait mailpit starts compose.yaml's catcher on the ports this run reads:" >&2
	echo "     PLATFORMKIT_MAILPIT_SMTP_PORT=${PLATFORMKIT_MAILPIT_SMTP_PORT:-1025} is the server the application dials," >&2
	echo "     PLATFORMKIT_MAILPIT_PORT=${PLATFORMKIT_MAILPIT_PORT:-8025} is this API. A catcher elsewhere is named by" >&2
	echo "     PLATFORMKIT_E2E_MAIL_HOST, PLATFORMKIT_E2E_MAIL_PORT and PLATFORMKIT_E2E_MAILPIT_URL." >&2
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

# A function rather than the heredoc in place, because the port appears twice in it
# and a run that lost the port it was offered between the choice and the bind
# rewrites the whole file for its next attempt rather than patching two lines.
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
# The one mail server this run dials, and the reason the journeys can read a link at
# all: kit/config refuses a host with no sender, so both halves are written here.
# No secret: a catcher accepts anything and authenticates to nobody.
mail:
  host: "$mail_host"
  port: $mail_port
  from: "PlatformKit E2E <e2e@platformkit.test>"
YAML
}
write_config

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
echo "e2e: one tenant and one administrator"
# --language is the tenant's own declaration, not the deployment's: e2e/localization.spec.ts
# drives a Portuguese browser and expects the sign-in page in Portuguese, and the only thing
# that makes a tenant served in a second language is somebody saying so. A tenant created
# without it is served in the one language the copy is written in.
(run_app bootstrap --config "$work/config.yaml" \
	--tenant e2e --host localhost --name "End to end" --admin-email admin@e2e.test \
	--language pt-PT) >/dev/null

# wait_healthy answers whether the application this run started served its probe
# within its bound, and is scripts/mobile_e2e.sh's function of the same name read
# against this script's log: 0 once this run's own process is serving it, 1 when that
# process died, 2 when nothing ever answered, 3 when a process this run did not start
# holds the port. The two pins that ask it a question
# (scripts/e2e_health_owner_test.sh, scripts/e2e_health_requires_own_listener_test.sh)
# extract this function from the committed file rather than retyping it, so the
# ownership helper it needs is reached here when the caller has not reached it first.
wait_healthy() { # bound in seconds
	local bound="$1" waited=0 owners=""
	if ! command -v port_listeners >/dev/null 2>&1; then
		# shellcheck source=scripts/free_port.sh
		. "${root:?}/scripts/free_port.sh"
	fi
	while [ "$waited" -lt "$bound" ]; do
		# The process is asked before /health is, and the answer is not trusted until
		# the operating system says the socket behind it belongs to this run. A listener
		# that took the port while the binary was being built answers the probe, and an
		# application that lost the bind answers nothing at all — the two are
		# indistinguishable from the client's side, which is why a 200 cannot be the
		# whole of what serving means here.
		if ! kill -0 "$app_pid" 2>/dev/null; then return 1; fi
		if curl -fsS "http://localhost:$port/health" >/dev/null 2>&1; then
			owners="$(port_listeners "$port")"
			if printf '%s\n' "$owners" | grep -qx "$app_pid"; then return 0; fi
			# Somebody else is answering, so this run's application can never bind the
			# number it was configured with. The caller names the holder beside the
			# application's own log, which by then says what the bind answered.
			if [ -n "$owners" ]; then
				return 3
			fi
		fi
		sleep 1
		waited=$((waited + 1))
	done
	return 2
}

serve() { # start the application on $port and wait for /health: the codes wait_healthy gives
	echo "e2e: serving on $port"
	run_app run --config "$work/config.yaml" >"$work/app.log" 2>&1 &
	app_pid=$!
	wait_healthy 60
}

# Nothing closes the window between the port being offered above and the application
# binding it here — no portable mechanism hands a bound socket to a process about to
# be exec'd, which is the note in scripts/free_port.sh. So a port this run chose for
# itself is asked for again when the bind refuses it, and a run that would have been
# refused a gate it did not cause moves; the same holds when the socket is found to
# belong to somebody else before the bind is even attempted. A port the caller named
# is never moved, and an application that died of anything but the port is reported
# as what it is rather than retried into the noise.
attempts=0
until serve; do
	status=$?
	moved=""
	if [ -z "$requested_port" ] && [ "$attempts" -lt 3 ] &&
		{ [ "$status" -eq 3 ] ||
			{ [ "$status" -eq 1 ] && grep -qi 'address already in use' "$work/app.log" 2>/dev/null; }; }; then
		moved="$port"
	fi
	if [ -n "$moved" ]; then
		attempts=$((attempts + 1))
		echo "e2e: $moved was taken while the application was starting; choosing another." >&2
		if [ "$status" -eq 3 ]; then
			# The application is still starting and will die on the bind; stop it here,
			# because a run that moves the port must not leave its own process behind
			# holding nothing and answering to nothing.
			kill "$app_pid" 2>/dev/null || true
			wait "$app_pid" 2>/dev/null || true
			app_pid=""
		fi
		if ! port="$(bind_free_port)"; then
			echo "e2e: no free loopback port to serve on." >&2
			exit 1
		fi
		write_config
		continue
	fi
	case "$status" in
	2) echo "e2e: the application never answered /health on $port:" >&2 ;;
	3) echo "e2e: /health answered on $port, but the socket behind the answer is not this run's application, which could not bind a port somebody else was holding:" >&2 ;;
	*) echo "e2e: the application stopped before it served:" >&2 ;;
	esac
	cat "$work/app.log" >&2
	if holders="$(port_listeners "$port" 2>/dev/null)" && [ -n "$holders" ]; then
		echo "e2e: $port is held by pid(s) ${holders//$'\n'/ }, which this run did not start; scripts/free_port.sh names who is meant to hold it" >&2
	fi
	exit 1
done

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
	PLATFORMKIT_E2E_MAILPIT_URL="$mailpit_url" \
	npx playwright test "${output_args[@]}" "$@"
