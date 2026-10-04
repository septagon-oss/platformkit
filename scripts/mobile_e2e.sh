#!/usr/bin/env bash
# The device journey: the application on a database of its own, the shell's pinned
# APK on a device, and the one Maestro flow this repository owns. See
# e2e/maestro/catalog.yaml for what the device asserts, and e2e/maestro/flows.json
# for what the mobile pillar declares.
#
# It is scripts/e2e.sh's shape with a device at the end of it rather than a browser,
# because everything before the device is the same problem: a database nobody else
# sees, migrated from nothing; one tenant and one administrator, created by
# `platformkit bootstrap`; the binary on a port; and a teardown that runs whichever
# step failed. What is new here is the last mile — the pin, the reverse, the flow,
# the report, and the refusal when any of them is missing.
#
# Nothing here is quiet. A missing tool, a missing /dev/kvm or an unpinned build
# exits non-zero and names the missing piece. A journey that silently skipped would
# leave the rate in the manifest looking like a number nobody earned — the same
# objection the Makefile states against a suite that skips the transport it ships.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

port="${PLATFORMKIT_MOBILE_PORT:-8098}"
admin_url="${PLATFORMKIT_TEST_ADMIN_URL:?the owner connection; make mobile-e2e exports it}"
app_url="${PLATFORMKIT_TEST_DATABASE_URL:?the application connection; make mobile-e2e exports it}"
database="platformkit_mobile_$(date +%s)_${RANDOM}_$$"
apk="${PK_MOBILE_APK:-}"
digest="${PK_MOBILE_APK_SHA256:-}"
avd="${PK_MOBILE_AVD:-}"

# The pin, before anything is downloaded or started. An unpinned binary must never
# run against a live tenant: this application holds a bootstrapped administrator, and
# an unpinned URL would install whatever its owner served at that moment.
if [ -z "$apk" ] || [ -z "$digest" ]; then
	echo "mobile-e2e: PK_MOBILE_APK and PK_MOBILE_APK_SHA256 are both required: which shell build this journey runs against is the mobile owner's to name, and an unpinned download is not a test." >&2
	exit 1
fi
if [[ ! "$digest" =~ ^[0-9a-fA-F]{64}$ ]]; then
	echo "mobile-e2e: PK_MOBILE_APK_SHA256 must be a SHA-256 digest, and this is ${#digest} characters." >&2
	exit 1
fi

# The Android tools are installed by the SDK and not always put on PATH: the host the
# pkit-ci label runs on has adb and the emulator under ~/Android/Sdk with no
# ANDROID_HOME exported at all. Found there is found; only a tool nowhere to be found
# is missing. Maestro is searched for the same reason and not with `command -v` alone:
# it is a Gradle start-up script plus a lib directory, so the host that has it keeps it
# under a tools prefix of its own — $MAESTRO_HOME where one is exported, and
# ~/.local/share/platformkit-tools/maestro/bin where this loop's tooling installs it —
# and none of those prefixes is on a CI shell's PATH. A tool that exists and is not
# looked for is a journey that reports itself impossible, which is the wrong answer to
# give twice.
for dir in "${ANDROID_HOME:-}" "${ANDROID_SDK_ROOT:-}" "$HOME/Android/Sdk"; do
	[ -n "$dir" ] || continue
	for part in platform-tools emulator; do
		[ -x "$dir/$part" ] && PATH="$dir/$part:$PATH"
	done
done
for dir in "${MAESTRO_HOME:-}" "${MAESTRO_HOME:+$MAESTRO_HOME/bin}" "${XDG_DATA_HOME:-$HOME/.local/share}/platformkit-tools/maestro/bin"; do
	[ -n "$dir" ] || continue
	[ -x "$dir/maestro" ] && PATH="$dir:$PATH"
done
export PATH

# Fail fast, naming the missing piece. The host the pkit-ci label runs on was measured
# to have /dev/kvm world-readable, an x86_64 system image and two AVDs; a runner that
# turns out not to have them is a runner-label decision (or the hosted-device fallback
# the specify note names), not a reason to skip a flow.
missing=()
for tool in adb emulator maestro; do
	command -v "$tool" >/dev/null || missing+=("$tool")
done
[ -r /dev/kvm ] || missing+=("/dev/kvm (readable)")
command -v psql >/dev/null || missing+=("psql")
command -v python3 >/dev/null || missing+=("python3")
if [ "${#missing[@]}" -gt 0 ]; then
	echo "mobile-e2e: this host cannot run a device journey; missing: ${missing[*]}. On the self-hosted runner that is a missing Android SDK or KVM in the image — fix the label, do not skip the flow." >&2
	exit 1
fi
# An emulator has to be running for adb to have a device to wait for. Starting it is
# the CI job's job — it is the one that knows the image, the AVD and the GPU — so this
# script asks for the name and refuses to guess one.
if [ -z "$avd" ] && ! adb devices | grep -qs 'device$'; then
	echo "mobile-e2e: no device is attached and PK_MOBILE_AVD names none. The CI job boots the emulator (see .gitea/workflows/mobile.yml); a developer boots one the same way." >&2
	exit 1
fi

# A URL with the database swapped for this run's own. Everything else — host, port,
# credentials, parameters — is whatever the suite already uses, so a deployment that
# needs an sslmode keeps it. A PostgreSQL URL is scheme://user:pass@host:port/name
# with optional ?params, so the name is the last path segment and nothing else needs
# parsing; no driver, no jq.
swap() {
	python3 - "$1" "$2" <<'SWAP'
import sys

url, name = sys.argv[1], sys.argv[2]
head, _, query = url.partition("?")
if "://" not in head or "/" not in head.split("://", 1)[1]:
	print("mobile-e2e: invalid PostgreSQL URL", file=sys.stderr)
	sys.exit(1)
cut = head.rindex("/")
sys.stdout.write(head[:cut] + "/" + name + (("?" + query) if query else ""))
SWAP
}
psql_admin() { psql "$(swap "$admin_url" postgres)" -v ON_ERROR_STOP=1 -q "$@"; }

work="$(mktemp -d)"
app_pid=""
created=false
cleanup() {
	status=$?
	if [ -n "$app_pid" ]; then
		kill "$app_pid" 2>/dev/null || true
		wait "$app_pid" 2>/dev/null || true
	fi
	if "$created"; then
		psql_admin -c "DROP DATABASE $database WITH (FORCE);" >/dev/null || echo "mobile-e2e: could not remove $database" >&2
	fi
	rm -rf "$work"
	return "$status"
}
trap cleanup EXIT

# Built, not `go run`, for the reason scripts/e2e.sh gives: killing the parent of a
# go run leaves the application holding the port for the next run to drive.
go build -o "$work/platformkit" ./apps/platformkit

echo "mobile-e2e: a database of its own"
psql_admin -c "CREATE DATABASE $database;" >/dev/null
created=true
# The role exists (apps/platformkit/postgres-init.sql made it); the grants are per
# database, so a fresh one needs them again.
psql "$(swap "$admin_url" "$database")" -v ON_ERROR_STOP=1 -q \
	-c "GRANT USAGE ON SCHEMA public TO platformkit_app;" \
	-c "ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO platformkit_app;" \
	-c "ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO platformkit_app;"

# The swapped URLs are computed here rather than inside the heredoc below: the
# configuration is then one plain block somebody can read, and a malformed URL fails
# at this line rather than inside a here-document.
app_dsn="$(swap "$app_url" "$database")"
migrate_dsn="$(swap "$admin_url" "$database")"
cat >"$work/config.yaml" <<YAML
server:
  addr: ":$port"
  public_host: "localhost:$port"
  docs: false
database:
  url: "$app_dsn"
  migrate_url: "$migrate_dsn"
nats:
  url: "${PLATFORMKIT_TEST_NATS_URL:-nats://localhost:4222}"
log:
  level: "warn"
audit:
  retention_days: 365
files:
  dir: "$work/files"
YAML

password="mobile-$(date +%s)-password"
# The device reaches the application through adb reverse below, so the address it is
# told to talk to is the device's own localhost — the same route the shell's own
# scripts/e2e/android.sh takes. --language is the tenant's own declaration, as in
# scripts/e2e.sh: a tenant created without a second language is served in one.
run_app() {
	for variable in "${!PLATFORMKIT_@}"; do unset "$variable"; done
	export PLATFORMKIT_BOOTSTRAP_PASSWORD="$password"
	exec "$work/platformkit" "$@"
}
echo "mobile-e2e: one tenant and one administrator"
(run_app bootstrap --config "$work/config.yaml" \
	--tenant e2e --host localhost --name "End to end" --admin-email admin@e2e.test \
	--language pt-PT) >/dev/null

if command -v ss >/dev/null && ss -ltn 2>/dev/null | grep -q ":$port "; then
	echo "mobile-e2e: something is already listening on $port; set PLATFORMKIT_MOBILE_PORT." >&2
	exit 1
fi

echo "mobile-e2e: serving on $port"
run_app run --config "$work/config.yaml" >"$work/app.log" 2>&1 &
app_pid=$!
# Liveness before the request, as in scripts/e2e.sh: a 200 from a process this
# script did not fork is not this script's health check, and a run whose own
# application died at the bind would otherwise go on to install and drive a
# stranger's installation on the device.
for _ in $(seq 1 60); do
	if ! kill -0 "$app_pid" 2>/dev/null; then
		echo "mobile-e2e: the application stopped before it served:" >&2
		cat "$work/app.log" >&2
		exit 1
	fi
	if curl -fsS "http://localhost:$port/health" >/dev/null 2>&1; then break; fi
	sleep 1
done

echo "mobile-e2e: the pinned shell build"
cached="$work/shell.apk"
curl -fsSL "$apk" -o "$cached"
if ! echo "$digest  $cached" | sha256sum --check --status; then
	echo "mobile-e2e: $apk is not the build PK_MOBILE_APK_SHA256 names; nothing is installed." >&2
	exit 1
fi

echo "mobile-e2e: one device, pointed at this application"
adb wait-for-device
adb reverse "tcp:$port" "tcp:$port"
adb install -r "$cached" >/dev/null

# The declared set, this repository's share of it. The manifest is the denominator of
# mobile_flow_pass_rate, so the flows are run from it and never from a list beside it.
specs=()
while IFS= read -r spec; do
	specs+=("$spec")
done < <(python3 - <<'FLOWS'
import json

with open("e2e/maestro/flows.json") as handle:
	declared = json.load(handle)["flows"]
for flow in declared:
	if flow["runs"] == "platformkit":
		print(flow["spec"])
FLOWS
)
if [ "${#specs[@]}" -eq 0 ]; then
	echo "mobile-e2e: e2e/maestro/flows.json declares no flow this repository runs." >&2
	exit 1
fi
echo "mobile-e2e: ${specs[*]}"

# --format junit is what the rate is computed from; the journey's own steps stay on
# the terminal for whoever is watching.
maestro test --format junit --output "$work/maestro.xml" \
	--env APP_ID=dev.septagon.platformkit.ci \
	--env "SERVER=http://localhost:$port" \
	--env EMAIL=admin@e2e.test \
	--env "PASSWORD=$password" \
	"${specs[@]}"

# The number, computed where the flows ran and by the same code that will report it:
# every flow this repository declares has to be in that report as a pass.
MAESTRO_JUNIT="$work/maestro.xml" go test ./apps/platformkit -run TestDeclaredMobileFlowsRan -count=1

# The work directory is gone when the trap runs, whichever step failed; a job that
# wants the report for its own log says where.
if [ -n "${PK_MOBILE_REPORT:-}" ]; then
	cp "$work/maestro.xml" "$PK_MOBILE_REPORT"
fi
