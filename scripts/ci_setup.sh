#!/usr/bin/env bash
# The fixture every CI job that talks to the kernel needs, written once.
#
# Usage: scripts/ci_setup.sh [nats] [app-role]
#
# Task T-0219 split ci.yml's single `check` job into four that the two kernel
# runners take in parallel. Three of them need the same broker and the same
# unprivileged role, and four copies of the same eight lines — docker run, health
# poll, role file — is a fixture that drifts: the NATS image was pinned in one
# place and the init file named in another, and the first copy to be edited would
# have started a server the suite was not written against. So the sequence lives
# here, in one list, and each job says which units its suite dials.
#
# What deliberately stays in the job: the `services: postgres:` block, because the
# runner owns that container and its health probe — the same shape mobile.yml has
# used since its first green run — and the object store, which belongs to the job
# that runs `make check` (modules/file's S3 adapter's cases fail rather than skip
# without one, and that module reads the arrangement out of the workflow text). A
# case in scripts/ci_setup_test.sh keeps the service blocks and the calls in step.
#
# Containers started here are attached to the job's own network (the runner gives
# every job a fresh one) under the alias the endpoint in the job's env names, which
# is what the `nats:` hostname in PLATFORMKIT_TEST_NATS_URL already means. Each
# container's id is appended to $GITHUB_OUTPUT as `<unit>=<id>` so the job's
# always() cleanup step can remove what this one started — never by name, which on
# a shared runner would reach whoever holds that name.
#
# CI_RESOURCES_FILE names the same output file for a rehearsal run outside a job
# (scripts/ci_setup_test.sh); CI_SETUP_PROBE=fake answers the readiness waits without
# a server, for the same rehearsal, and fakes nothing else. Nothing reads either
# variable except this file and that one.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
resources="${GITHUB_OUTPUT:-${CI_RESOURCES_FILE:-}}"
probe="${CI_SETUP_PROBE:-}"

# The image the workflow pinned before this file existed, digest for digest.
# compose.yaml runs the same server by tag, because a workstation stack follows the
# developer's own pull; a gate pins, so the server a run tested is named by the run.
nats_image='nats:2-alpine@sha256:ad7a43eb7e3337c3c38ce5d784d1461791f95f730f252d2b25eee699752a0ca3'

say() { echo "ci-setup: $*" >&2; }

if [ "$#" -eq 0 ]; then
	say "usage: $0 [nats] [app-role]; a job names the units its suite dials"
	exit 2
fi
if [ -z "$resources" ]; then
	say "no output file: GITHUB_OUTPUT is unset, so nothing could hand the ids to the cleanup step"
	exit 2
fi
network="${JOB_NETWORK:-}"
if [ -z "$network" ] && [ "$probe" != "fake" ]; then
	say "JOB_NETWORK is empty: this job has no network to attach a server to"
	exit 2
fi

# :ready waits for something that answers on the job's network. Every wait is on the
# server's own health endpoint, never on a sleep: the question is "may I connect",
# and the answer comes from the thing being asked. Thirty seconds is the bound the
# workflow used before this file existed; the images are digest-pinned, so what is
# being waited for is a start, not a pull.
ready() { # ready <name> <probe command…>
	local name="$1"; shift
	local i
	for i in $(seq 1 30); do
		if [ "$probe" = "fake" ]; then return 0; fi
		if "$@" >/dev/null 2>&1; then return 0; fi
		sleep 1
	done
	say "$name did not answer within 30s"
	return 1
}

# The database client arrives before anything that speaks SQL: the job container is a
# plain image with no psql in it, which is why the workflow installed it as a step of
# its own until this file took the step over.
if ! command -v pg_isready >/dev/null 2>&1 || ! command -v psql >/dev/null 2>&1; then
	apt-get update
	apt-get install --yes --no-install-recommends postgresql-client
fi

for unit in "$@"; do
	case "$unit" in
	nats)
		container=$(docker run -d --network "$network" --network-alias nats \
			"$nats_image" --jetstream --http_port 8222)
		printf 'nats=%s\n' "$container" >>"$resources"
		ready nats curl -fsS http://nats:8222/healthz
		;;
	app-role)
		# The unprivileged role the application connection authenticates as, from the
		# file the deployment ships. It waits for the database first: the service and
		# this step are two halves of one arrangement, and a role applied to a server
		# that is still starting is a failure that reads like a broken migration.
		# The file is idempotent (`DO $$ … IF NOT EXISTS`), so a job may run it twice.
		ready postgres pg_isready -h postgres -U postgres -d platformkit
		PGPASSWORD=platformkit psql -h postgres -U postgres -d platformkit -v ON_ERROR_STOP=1 \
			-f apps/platformkit/postgres-init.sql
		;;
	postgres)
		say "Postgres is the job's own service, so the runner owns its health probe; this file starts the broker and the role"
		exit 2
		;;
	s3)
		say "the object store is started by the job that runs make check, where modules/file's own case reads it: see .gitea/workflows/ci.yml"
		exit 2
		;;
	*)
		say "unknown unit '$unit': this file starts nats and applies app-role, nothing else"
		exit 2
		;;
	esac
done
