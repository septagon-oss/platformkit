#!/usr/bin/env bash
# One command from a fresh machine to a running application.
#
# It is the five README commands with the checks a person would otherwise
# discover one failure at a time: the two tools that have to be installed, the
# two ports that have to be published, the configuration file that has to exist,
# and the first tenant that has to be created exactly once.
#
# Running it twice is running it once. The compose stack is already up, the
# configuration file is already there, and `bootstrap` refuses an installation
# that has a tenant — so the second run does nothing but start the application.
#
# It never uses sudo. Everything it needs is either already installed or
# something only the machine's owner should install.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

run=true
for arg in "$@"; do
	case "$arg" in
	--no-run) run=false ;;
	-h | --help)
		cat <<'USAGE'
scripts/start.sh — one command from a fresh machine to a running application.

usage: scripts/start.sh [--no-run]
  --no-run   set everything up and stop before serving

It checks docker and go, starts Postgres and NATS, copies config.example.yaml
to config.yaml if there is none, creates the first tenant, and runs the app.
Running it twice is running it once.

env: PLATFORMKIT_PG_PORT (5432), PLATFORMKIT_NATS_PORT (4222)
USAGE
		exit 0
		;;
	*)
		echo "start: $arg is not an argument; there is one, --no-run" >&2
		exit 2
		;;
	esac
done

say() { printf 'start: %s\n' "$*"; }
die() {
	printf 'start: %s\n' "$*" >&2
	exit 1
}

# --- what has to be installed ------------------------------------------------

command -v docker >/dev/null || die "docker is not installed. Postgres and NATS run in it; see compose.yaml."
docker compose version >/dev/null 2>&1 || die "docker is installed but 'docker compose' is not. Install the compose plugin."
docker info >/dev/null 2>&1 || die "the docker daemon is not answering. Start it, or add yourself to the docker group."

command -v go >/dev/null || die "go is not installed. See https://go.dev/dl."
# The version the module asks for, against the version the toolchain reports.
# GOVERSION carries the build's own suffixes on a patched toolchain, so it is
# cut back to the three numbers before either is compared.
want="$(awk '/^go /{print $2; exit}' go.mod)"
have="$(go env GOVERSION)"
have="${have#go}"
have="${have%%-*}"
if [ "$(printf '%s\n%s\n' "$want" "$have" | sort -V | head -1)" != "$want" ]; then
	die "go $have is installed and go.mod asks for $want or newer."
fi
say "docker, and go $have for a module that asks for $want"

# node is gate 10's and nothing else's, so its absence is a line rather than an
# exit: an application that runs is what this script is for.
if command -v node >/dev/null; then
	say "node $(node --version) is here, so 'make e2e' can run too"
else
	say "node is not installed, so 'make e2e' (gate 10) cannot run here. Nothing else needs it."
fi

# --- the two ports -----------------------------------------------------------

export PLATFORMKIT_PG_PORT="${PLATFORMKIT_PG_PORT:-5432}"
export PLATFORMKIT_NATS_PORT="${PLATFORMKIT_NATS_PORT:-4222}"

# config.example.yaml names the default ports, because it is the file a first
# run copies and a first run has no reason to move them. Overriding a port
# therefore has to reach the application some other way, and kit/config's
# environment overrides are that way: they leave config.yaml alone, which
# matters because after the first run it is the reader's file and not ours.
overrides=()
if [ "$PLATFORMKIT_PG_PORT" != 5432 ]; then
	: "${PLATFORMKIT_DATABASE_URL:=postgres://platformkit_app:platformkit@localhost:$PLATFORMKIT_PG_PORT/platformkit?sslmode=disable&connect_timeout=5}"
	: "${PLATFORMKIT_DATABASE_MIGRATE_URL:=postgres://postgres:platformkit@localhost:$PLATFORMKIT_PG_PORT/platformkit?sslmode=disable&connect_timeout=5}"
	export PLATFORMKIT_DATABASE_URL PLATFORMKIT_DATABASE_MIGRATE_URL
	overrides+=("PLATFORMKIT_DATABASE_URL=$PLATFORMKIT_DATABASE_URL" "PLATFORMKIT_DATABASE_MIGRATE_URL=$PLATFORMKIT_DATABASE_MIGRATE_URL")
fi
if [ "$PLATFORMKIT_NATS_PORT" != 4222 ]; then
	: "${PLATFORMKIT_NATS_URL:=nats://localhost:$PLATFORMKIT_NATS_PORT}"
	export PLATFORMKIT_NATS_URL
	overrides+=("PLATFORMKIT_NATS_URL=$PLATFORMKIT_NATS_URL")
fi

# --- Postgres and NATS -------------------------------------------------------

say "make up (postgres on $PLATFORMKIT_PG_PORT, nats on $PLATFORMKIT_NATS_PORT)"
make up

# `docker compose up --wait` waits for the health checks, which run inside the
# containers, and a container can be healthy on a port that was never published:
# a bind that loses a race with another process leaves exactly that state, and
# the next thing to fail is the migration, forty lines later, as a connection
# refused. So readiness is measured from here, over the published port, which is
# where the application will reach them from.
ready() { # name port
	for _ in $(seq 1 60); do
		if (exec 3<>"/dev/tcp/127.0.0.1/$2") 2>/dev/null; then
			exec 3<&- 3>&-
			say "$1 answers on $2"
			return 0
		fi
		sleep 1
	done
	die "$1 did not answer on 127.0.0.1:$2 within a minute. 'docker compose ps' and 'docker compose logs $1' say why; a port already in use is the usual reason, and PLATFORMKIT_$3_PORT moves it."
}
ready postgres "$PLATFORMKIT_PG_PORT" PG
ready nats "$PLATFORMKIT_NATS_PORT" NATS

# --- configuration -----------------------------------------------------------

if [ -f config.yaml ]; then
	say "config.yaml is already here, so it is left alone"
else
	cp config.example.yaml config.yaml
	say "config.yaml copied from config.example.yaml"
fi

# --- the first tenant --------------------------------------------------------

# The password is printed by `bootstrap` itself, to stderr, once. It is captured
# here only so that the refusal can be told apart from a real failure, and it is
# printed back unchanged: this script never learns the password and never writes
# it anywhere.
say "bootstrap"
if out="$(go run ./apps/platformkit bootstrap --config config.yaml \
	--tenant platformkit --host platformkit.localhost \
	--name PlatformKit --admin-email admin@platformkit.localhost 2>&1)"; then
	printf '%s\n' "$out"
elif printf '%s' "$out" | grep -q 'already has'; then
	say "this installation already has a tenant, so bootstrap refused; that is the idempotent path"
else
	printf '%s\n' "$out" >&2
	die "bootstrap failed."
fi

# --- serve -------------------------------------------------------------------

# /admin/login rather than /admin: an anonymous request to a screen is
# refused, and refused in problem+json, so a browser sent to /admin first is
# shown a JSON object. The sign-in page is the door.
say "sign in at http://platformkit.localhost:8080/admin/login as admin@platformkit.localhost"
if [ "$run" = false ]; then
	say "--no-run, so nothing is serving yet. Start it with:"
	# The overrides are printed shell-quoted because a Postgres URL holds a `&`,
	# and a line somebody pastes has to be a line that runs.
	line=""
	for o in ${overrides[@]+"${overrides[@]}"}; do line="$line $(printf %q "$o")"; done
	if [ -z "$line" ]; then
		printf '\n    make run\n\n'
	else
		printf '\n    env%s make run\n\n' "$line"
	fi
	exit 0
fi
say "make run — ^C stops it, 'make down' removes the database"
exec make run
