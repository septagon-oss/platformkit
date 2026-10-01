#!/usr/bin/env bash
# ci_setup_test.sh — the cases scripts/ci_setup.sh and scripts/ci_cleanup.sh must pass.
#
# The job split (task T-0219) put the fixture every CI job needs into a script, so
# the file that decides whether a gate has a database is now load-bearing and had
# better be runnable without a runner. Nothing here talks to Docker, Postgres or
# NATS: a fake `docker` on PATH answers what the containers were called, and the
# script's own CI_SETUP_PROBE branch stands in for the health waits. What is under
# test is the part a runner cannot forgive — which units got started, under which
# alias, from which pinned image, and which ids got handed to the cleanup step.
#
# `make check` runs this file beside the architecture and budget rehearsals.
set -Eeuo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
setup="$root/scripts/ci_setup.sh"
cleanup="$root/scripts/ci_cleanup.sh"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT
failed=0
fail() { echo "FAIL: $*" >&2; failed=1; }
say_ok() { if [ "$failed" = "${start:-0}" ]; then echo "ok   $*"; fi; }

# The fake docker: it prints an id built from its arguments' last word, so an
# assertion can tell which unit this call was, and it records every argv.
mkdir -p "$fixture/bin"
cat >"$fixture/bin/docker" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$DOCKER_LOG"
alias=""
while [ "$#" -gt 0 ]; do
	if [ "$1" = "--network-alias" ]; then alias="$2"; fi
	shift
done
echo "fake-${alias:-none}-1"
EOF
chmod +x "$fixture/bin/docker"
# The database client and the probe exist, so the setup script never reaches for
# apt-get in a rehearsal.
for tool in pg_isready psql curl; do
	printf '#!/usr/bin/env bash\necho "%s $*" >>"$TOOL_LOG"\nexit 0\n' "$tool" >"$fixture/bin/$tool"
	chmod +x "$fixture/bin/$tool"
done

export PATH="$fixture/bin:$PATH"
export DOCKER_LOG="$fixture/docker.log"
: >"$DOCKER_LOG"

start=$failed
# case 1 — a job that names no unit gets the usage and exit 2, not a silent success.
out="$(CI_RESOURCES_FILE="$fixture/res" JOB_NETWORK=net CI_SETUP_PROBE=fake bash "$setup" 2>&1)" \
	&& code=0 || code=$?
[ "$code" = 2 ] || fail "no units: exit $code, want 2"
grep -q 'usage:' <<<"$out" || fail "no units: the answer names no usage: $out"
say_ok "no units is refused with the usage, before a container is run"

start=$failed
# case 2 — no output file means no way to hand the ids to the cleanup step.
out="$(env -u GITHUB_OUTPUT -u CI_RESOURCES_FILE JOB_NETWORK=net CI_SETUP_PROBE=fake bash "$setup" postgres 2>&1)" \
	&& code=0 || code=$?
[ "$code" = 2 ] || fail "no output file: exit $code, want 2"
grep -q 'GITHUB_OUTPUT is unset' <<<"$out" || fail "no output file: $out"
say_ok "a job with nowhere to record its containers refuses instead of leaking them"

start=$failed
# case 3 — an empty network is the runner not having given one.
out="$(CI_RESOURCES_FILE="$fixture/res" JOB_NETWORK= bash "$setup" postgres 2>&1)" && code=0 || code=$?
[ "$code" = 2 ] || fail "empty network: exit $code, want 2"
grep -q 'JOB_NETWORK is empty' <<<"$out" || fail "empty network: $out"
say_ok "an empty JOB_NETWORK is named, not worked around"

start=$failed
# case 4 — an unknown unit is a typo in a workflow, and a typo that started nothing
# is a red gate three minutes later.
out="$(CI_RESOURCES_FILE="$fixture/res" JOB_NETWORK=net CI_SETUP_PROBE=fake bash "$setup" postgres kafka 2>&1)" \
	&& code=0 || code=$?
[ "$code" = 2 ] || fail "unknown unit: exit $code, want 2"
grep -q "unknown unit 'kafka'" <<<"$out" || fail "unknown unit: $out"
say_ok "a unit this file does not start is refused by name"

start=$failed
# case 5 — the object store belongs to the job that runs `make check`, where
# modules/file's own case reads it out of the workflow text. Starting it from here
# would move it out of that case's reach.
out="$(CI_RESOURCES_FILE="$fixture/res" JOB_NETWORK=net CI_SETUP_PROBE=fake bash "$setup" s3 2>&1)" \
	&& code=0 || code=$?
[ "$code" = 2 ] || fail "s3: exit $code, want 2"
grep -q 'ci.yml' <<<"$out" || fail "s3: the answer does not say where the store is started: $out"
say_ok "the object store is not this script's, and says who owns it"

start=$failed
# case 6 — the two units a suite dials: one line each in the output file, each under
# the alias the job's endpoint names, each from the pinned image.
: >"$fixture/tools.log"
: >"$DOCKER_LOG"
: >"$fixture/res"
CI_RESOURCES_FILE="$fixture/res" JOB_NETWORK=net CI_SETUP_PROBE=fake bash "$setup" postgres nats >/dev/null
res="$(cat "$fixture/res")"
[ "$(wc -l <"$fixture/res")" = 2 ] || fail "two units: $(wc -l <"$fixture/res") lines, want 2"
grep -q '^postgres=fake-postgres-1$' <<<"$res" || fail "no postgres output line: $res"
grep -q '^nats=fake-nats-1$' <<<"$res" || fail "no nats output line: $res"
grep -q -- '--network-alias postgres' "$DOCKER_LOG" || fail "postgres started under no alias"
grep -q -- '--network-alias nats' "$DOCKER_LOG" || fail "nats started under no alias"
grep -q 'postgres:16@sha256:' "$DOCKER_LOG" || fail "postgres started from no pinned image"
grep -q 'nats:2-alpine@sha256:' "$DOCKER_LOG" || fail "nats started from no pinned image"
grep -q -- '--jetstream' "$DOCKER_LOG" || fail "nats started without JetStream, which every event case needs"
# The ids handed out are the ids docker answered with — cleanup can only remove what
# it was told about, and the fake prints the alias it was given.
grep -q -- '--network-alias postgres' "$DOCKER_LOG" && grep -q 'postgres=fake-postgres-1' "$fixture/res" \
	|| fail "the id in the output file is not the one docker printed"
say_ok "two units, two aliases, two pinned images, and the printed ids in the output file"

start=$failed
# case 7 — with the health answers real (the fake tools exit 0), the app role is
# created from the file the deployment ships.
export TOOL_LOG="$fixture/tools2.log"
: >"$fixture/tools2.log"
: >"$DOCKER_LOG"
: >"$fixture/res"
CI_RESOURCES_FILE="$fixture/res" JOB_NETWORK=net bash "$setup" postgres nats >/dev/null
grep -q 'psql -h postgres' "$fixture/tools2.log" || fail "the app role was never created: $(cat "$fixture/tools2.log")"
grep -q 'postgres-init.sql' "$fixture/tools2.log" || fail "psql ran with no init file"
grep -q 'pg_isready -h postgres' "$fixture/tools2.log" || fail "postgres was never asked whether it was ready"
say_ok "the role comes from apps/platformkit/postgres-init.sql, after the readiness probe"

start=$failed
# case 8 — cleanup removes the ids it was handed, says nothing about the ones a
# failed setup never produced, and fails when one is still there.
export TOOL_LOG="$fixture/clean.log"
: >"$TOOL_LOG"
printf '#!/usr/bin/env bash\necho "$*" >>"$TOOL_LOG"\nif [ "$*" = "rm -f gone" ]; then exit 1; fi\nexit 0\n' \
	>"$fixture/bin/docker"
chmod +x "$fixture/bin/docker"
: >"$TOOL_LOG"
bash "$cleanup" one "" three >/dev/null && code=0 || code=$?
[ "$code" = 0 ] || fail "cleanup of two live ids exited $code, want 0"
grep -q 'rm -f one' "$TOOL_LOG" || fail "cleanup did not remove the id it was handed"
grep -q 'rm -f three' "$TOOL_LOG" || fail "cleanup did not remove the second id"
[ "$(grep -c 'rm ' "$TOOL_LOG")" = 2 ] || fail "cleanup removed an id nobody handed it: $(cat "$TOOL_LOG")"
: >"$TOOL_LOG"
bash "$cleanup" gone >/dev/null && code=0 || code=$?
[ "$code" = 1 ] || fail "a container that would not go away exited $code, want 1"
say_ok "cleanup takes ids, skips the empty ones, and reports the one it could not remove"

if [ "$failed" = 0 ]; then
	echo "ci setup: every case holds"
else
	echo "ci setup: a case failed" >&2
fi
exit "$failed"
