#!/usr/bin/env bash
# Gate 10: the admin shell renders and a generated CRUD screen works.
#
# It boots the application the way a person would and then drives it with a
# browser: a database of its own, migrated from nothing; one tenant and one
# administrator, created by `platformkit bootstrap`; the binary on a port; one
# Playwright spec; and then all of it removed again. Nothing it touches survives
# it, so running it twice is running it once.
#
# It is a script rather than four lines in the Makefile because the teardown has
# to happen whichever step failed, and a recipe cannot trap.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

port="${PLATFORMKIT_E2E_PORT:-8099}"
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
app_pid=""
created=false
cleanup() {
	if [ -n "$app_pid" ]; then
		kill "$app_pid" 2>/dev/null || true
		wait "$app_pid" 2>/dev/null || true
	fi
	if "$created"; then
		psql_admin -c "DROP DATABASE $database WITH (FORCE);" >/dev/null || echo "e2e: could not remove $database" >&2
	fi
	rm -rf "$work"
}
trap cleanup EXIT

# The binary is built rather than `go run`: go run execs the compiled program as
# a child, so killing it at the end of this script would leave the application
# holding the port and the next run would drive the previous run's build.
go build -o "$work/platformkit" ./apps/platformkit

echo "e2e: a database of its own"
psql_admin -c "CREATE DATABASE $database;" >/dev/null
created=true
# The role exists (apps/platformkit/postgres-init.sql made it); the grants are per
# database, so a fresh one needs them again.
psql "$(swap "$admin_url")" -v ON_ERROR_STOP=1 -q \
	-c "GRANT USAGE ON SCHEMA public TO platformkit_app;" \
	-c "ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO platformkit_app;" \
	-c "ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO platformkit_app;"

cat >"$work/config.yaml" <<YAML
server:
  addr: "127.0.0.1:$port"
  public_host: "localhost:$port"
  docs: false
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

password="e2e-$(date +%s)-password"
export PLATFORMKIT_BOOTSTRAP_PASSWORD="$password"
echo "e2e: one tenant and one administrator"
"$work/platformkit" bootstrap --config "$work/config.yaml" \
	--tenant e2e --host localhost --name "End to end" --admin-email admin@e2e.test >/dev/null

if command -v ss >/dev/null && ss -ltn 2>/dev/null | grep -q ":$port "; then
	echo "e2e: something is already listening on $port; set PLATFORMKIT_E2E_PORT." >&2
	exit 1
fi

echo "e2e: serving on $port"
"$work/platformkit" run --config "$work/config.yaml" >"$work/app.log" 2>&1 &
app_pid=$!
for _ in $(seq 1 60); do
	if curl -fsS "http://localhost:$port/health" >/dev/null 2>&1; then break; fi
	if ! kill -0 "$app_pid" 2>/dev/null; then
		echo "e2e: the application stopped before it served:" >&2
		cat "$work/app.log" >&2
		exit 1
	fi
	sleep 1
done

cd e2e
# npm ci and not npm install: ci installs exactly what package-lock.json pins
# and fails when the lock and the manifest disagree, which is what a gate wants.
# install resolves afresh and rewrites the lock, so gate 10 could pass against a
# Playwright nobody chose and leave the lock changed in somebody's working tree.
[ -d node_modules ] || npm ci --no-audit --no-fund
PLATFORMKIT_E2E_URL="http://localhost:$port" \
	PLATFORMKIT_E2E_EMAIL="admin@e2e.test" \
	PLATFORMKIT_E2E_PASSWORD="$password" \
	npx playwright test --output "$work/results" "$@"
