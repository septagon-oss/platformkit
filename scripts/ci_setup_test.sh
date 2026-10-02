#!/usr/bin/env bash
# ci_setup_test.sh — the cases scripts/ci_setup.sh and scripts/ci_cleanup.sh must pass.
#
# The job split (task T-0219) put the fixture every CI job needs into a script, so
# the file that decides whether a gate has a database is now load-bearing and had
# better be runnable without a runner. Nothing here talks to Docker, Postgres or
# NATS: a fake `docker` on PATH answers what the containers were called, and the
# script's own CI_SETUP_PROBE branch stands in for the health waits — and the probe
# branch fakes only the waits: every docker, pg_isready and psql call still happens and
# is logged, which is the part a runner cannot forgive: which units got started, under
# which alias, from which pinned image, and which ids got handed to the cleanup step.
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
out="$(env -u GITHUB_OUTPUT -u CI_RESOURCES_FILE JOB_NETWORK=net CI_SETUP_PROBE=fake bash "$setup" nats 2>&1)" \
	&& code=0 || code=$?
[ "$code" = 2 ] || fail "no output file: exit $code, want 2"
grep -q 'GITHUB_OUTPUT is unset' <<<"$out" || fail "no output file: $out"
say_ok "a job with nowhere to record its containers refuses instead of leaking them"

start=$failed
# case 3 — an empty network is the runner not having given one.
out="$(CI_RESOURCES_FILE="$fixture/res" JOB_NETWORK= bash "$setup" nats 2>&1)" && code=0 || code=$?
[ "$code" = 2 ] || fail "empty network: exit $code, want 2"
grep -q 'JOB_NETWORK is empty' <<<"$out" || fail "empty network: $out"
say_ok "an empty JOB_NETWORK is named, not worked around"

start=$failed
# case 4 — an unknown unit is a typo in a workflow, and a typo that started nothing
# is a red gate three minutes later.
out="$(CI_RESOURCES_FILE="$fixture/res" JOB_NETWORK=net CI_SETUP_PROBE=fake bash "$setup" nats kafka 2>&1)" \
	&& code=0 || code=$?
[ "$code" = 2 ] || fail "unknown unit: exit $code, want 2"
grep -q "unknown unit 'kafka'" <<<"$out" || fail "unknown unit: $out"
say_ok "a unit this file does not start is refused by name"

start=$failed
# case 5 — the two units a job dials: the broker's id in the output file under the
# alias the job's endpoint names, from the pinned image, with JetStream on; and the app
# role applied from the file the deployment ships, after the database answered.
: >"$fixture/tools.log"
: >"$DOCKER_LOG"
: >"$fixture/res"
export TOOL_LOG="$fixture/tools.log"
CI_RESOURCES_FILE="$fixture/res" JOB_NETWORK=net CI_SETUP_PROBE=fake bash "$setup" nats app-role >/dev/null
res="$(cat "$fixture/res")"
[ "$(wc -l <"$fixture/res")" = 1 ] || fail "nats app-role: $(wc -l <"$fixture/res") output lines, want the broker's one: $res"
grep -q '^nats=fake-nats-1$' <<<"$res" || fail "no nats output line: $res"
grep -q -- '--network-alias nats' "$DOCKER_LOG" || fail "nats started under no alias"
grep -q 'nats:2-alpine@sha256:' "$DOCKER_LOG" || fail "nats started from no pinned image"
grep -q -- '--jetstream' "$DOCKER_LOG" || fail "nats started without JetStream, which every event case needs"
grep -q 'psql -h postgres' "$fixture/tools.log" || fail "the app role was never created: $(cat "$fixture/tools.log")"
grep -q 'postgres-init.sql' "$fixture/tools.log" || fail "psql ran with no init file"
! grep -q -- '--name ' "$DOCKER_LOG" || fail "a broker was named when the job named nothing"
say_ok "the broker's id, alias and pin, and the role applied from the deployment's own file"

start=$failed
# case 5b — the name a job gave its broker. An id is a handle a step the job's own
# deadline cuts off never finishes writing (main learned that on run 245, and its
# container held the run's image for the rest of the job), so the workflow names the
# container from values its cleanup step can rebuild. This case is that the name
# reaches the container, and that whatever held the name is dropped first, so a
# second `docker run` says so on the spot instead of leaving the suite without a broker.
: >"$DOCKER_LOG"
: >"$fixture/res"
NATS_CONTAINER_NAME=platformkit-77-go-checks-nats CI_RESOURCES_FILE="$fixture/res" \
	JOB_NETWORK=net CI_SETUP_PROBE=fake bash "$setup" nats >/dev/null
grep -q -- '--name platformkit-77-go-checks-nats' "$DOCKER_LOG" || fail "the broker ran unnamed: $(cat "$DOCKER_LOG")"
grep -q '^rm -f platformkit-77-go-checks-nats$' "$DOCKER_LOG" || fail "nothing dropped the name before the run: $(cat "$DOCKER_LOG")"
grep -q '^nats=fake-nats-1$' "$fixture/res" || fail "the named broker left the cleanup step no id either: $(cat "$fixture/res")"
say_ok "a broker the job named is named on the container, and the name is cleared first"

# case 6 — with the health answers not faked, both units ask the server first: the
# role step waits for the service rather than racing it.
export TOOL_LOG="$fixture/tools2.log"
: >"$fixture/tools2.log"
: >"$DOCKER_LOG"
: >"$fixture/res"
CI_RESOURCES_FILE="$fixture/res" JOB_NETWORK=net bash "$setup" nats app-role >/dev/null
grep -q 'curl -fsS http://nats:8222/healthz' "$fixture/tools2.log" || fail "nats was never asked whether it was up"
grep -q 'pg_isready -h postgres' "$fixture/tools2.log" || fail "the role step never probed the database"
say_ok "each unit asks the server the question, instead of sleeping and hoping"

# case 7 — Postgres is the job's service and the object store is another job's step.
# Both are units a workflow author could name by mistake, and each mistake would leave a
# job with two databases or no store, three minutes from now.
out="$(CI_RESOURCES_FILE="$fixture/res" JOB_NETWORK=net CI_SETUP_PROBE=fake bash "$setup" postgres 2>&1)" \
	&& code=0 || code=$?
[ "$code" = 2 ] || fail "postgres as a unit: exit $code, want 2"
grep -q 'the job.s own service' <<<"$out" || fail "postgres as a unit: $out"
out="$(CI_RESOURCES_FILE="$fixture/res" JOB_NETWORK=net CI_SETUP_PROBE=fake bash "$setup" s3 2>&1)" \
	&& code=0 || code=$?
[ "$code" = 2 ] || fail "s3 as a unit: exit $code, want 2"
grep -q 'ci.yml' <<<"$out" || fail "s3 as a unit: the answer does not say where the store is started: $out"
say_ok "the database and the object store name the job that owns each"

# case 8 — the workflow and this script in step: every job that calls it declares a
# database of its own, and no job calls it for a unit this file refuses. Three calls,
# three service blocks, one workflow file.
workflow="${CI_WORKFLOW_FILE:-$root/.gitea/workflows/ci.yml}"
calls=$(grep -c 'bash scripts/ci_setup.sh' "$workflow")
services=$(grep -cE '^      postgres:$' "$workflow")
[ "$calls" = "$services" ] || fail "$calls jobs call scripts/ci_setup.sh but $services declare a postgres service"
grep -E 'bash scripts/ci_setup.sh' "$workflow" | grep -vE 'scripts/ci_setup\.sh (nats )?(nats )?app-role$|scripts/ci_setup.sh nats app-role$' \
	&& fail "a job calls this script with a unit it does not start"
# A broker a job starts by hand has to be a container that job can still point at
# after a step it does not finish: the name is the handle, and it has to carry both
# the run and the job, because three jobs of one run each start a broker and a name
# one of them pre-cleared would be another one's server.
for job in go-checks race-and-vuln e2e; do
	name="platformkit-\${{ github.run_id }}-$job-nats"
	grep -qF "NATS_CONTAINER_NAME: $name" "$workflow" \
		|| fail "$job starts a broker under no name its cleanup step could rebuild"
done
# A valid URL still fails the suite if the role step is moved below the gate.
for pair in 'go-checks|make check' 'race-and-vuln|make check-race' 'e2e|make e2e'; do
	job="${pair%%|*}"
	goal="${pair#*|}"
	if ! awk -v job="$job" -v goal="$goal" '
		$0 == "  " job ":" { found = 1; inside = 1; next }
		inside && /^  [a-z0-9-]+:/ { exit }
		inside && /run: bash scripts\/ci_setup.sh nats app-role$/ { setup = NR }
		inside && $0 ~ ("run: " goal "$") { gate = NR }
		END { if (!found || !setup || !gate || setup >= gate) exit 1 }
	' "$workflow"; then
		fail "$job must create the application role before $goal"
	fi
done
say_ok "every job that calls the script declares the database it points at"

# case 9 — cleanup removes the handles it was handed, says nothing about the ones a
# failed setup never produced, reports the one it could not remove, and skips a handle
# that names no container on this daemon (the step that would have made it may never
# have run, and a handle for a container that is already gone is not a failure).
export TOOL_LOG="$fixture/clean.log"
: >"$TOOL_LOG"
printf '#!/usr/bin/env bash\necho "$*" >>"$TOOL_LOG"\nif [ "$1" = inspect ]; then [ "$2" = ghost ] && exit 1; exit 0; fi\nif [ "$*" = "rm -f gone" ]; then exit 1; fi\nexit 0\n' \
	>"$fixture/bin/docker"
chmod +x "$fixture/bin/docker"
: >"$TOOL_LOG"
bash "$cleanup" one "" three >/dev/null && code=0 || code=$?
[ "$code" = 0 ] || fail "cleanup of two live ids exited $code, want 0"
grep -q 'rm -f one' "$TOOL_LOG" || fail "cleanup did not remove the id it was handed"
grep -q 'rm -f three' "$TOOL_LOG" || fail "cleanup did not remove the second id"
[ "$(grep -c 'rm ' "$TOOL_LOG")" = 2 ] || fail "cleanup removed an id nobody handed it: $(cat "$TOOL_LOG")"
: >"$TOOL_LOG"
bash "$cleanup" ghost >/dev/null && code=0 || code=$?
[ "$code" = 0 ] || fail "a handle naming no container exited $code, want 0"
[ "$(grep -c 'rm ' "$TOOL_LOG")" = 0 ] || fail "cleanup tried to remove a container it was told is not there: $(cat "$TOOL_LOG")"
: >"$TOOL_LOG"
bash "$cleanup" gone >/dev/null && code=0 || code=$?
[ "$code" = 1 ] || fail "a container that would not go away exited $code, want 1"
say_ok "cleanup takes ids and names, skips the empty ones and the ones naming nothing, and reports the one it could not remove"

# case 10 — the shared store arrives with the suite that dials it. kit/cache's
# conformance case, kit/httpx's host-invalidation connections and apps/platformkit's
# suspension across a second process all fail rather than skip once
# PLATFORMKIT_TEST_VALKEY_URL is set — an address a job sets for itself is a promise.
# One job on main carried both halves; four jobs is where a suite quietly gets back
# the skip it was written not to need, so the job that runs the goal owes both the
# address in its env and the container that answers it.
for pair in 'go-checks|make check' 'race-and-vuln|make check-race'; do
	job="${pair%%|*}"
	goal="${pair#*|}"
	if ! awk -v job="$job" -v goal="$goal" '
		$0 == "  " job ":" { found = 1; inside = 1; next }
		inside && /^  [a-z0-9-]+:/ { exit }
		inside && /PLATFORMKIT_TEST_VALKEY_URL: redis:\/\/valkey:6379/ { url = 1 }
		inside && /--network-alias valkey/ { store = 1 }
		inside && $0 ~ ("run: " goal "$") { ran = 1 }
		END { if (!found || !ran || !url || !store) exit 1 }
	' "$workflow"; then
		fail "$job runs $goal without the shared store its cases dial"
	fi
done
say_ok "each Go job that dials the shared store starts it and names its address"

if [ "$failed" = 0 ]; then
	echo "ci setup: every case holds"
else
	echo "ci setup: a case failed" >&2
fi
exit "$failed"
