#!/usr/bin/env bash
# The fixture every CI job that talks to the kernel needs, written once.
#
# Usage: scripts/ci_setup.sh [postgres] [nats]
#
# Task T-0219 split ci.yml's single `check` job into four that the two kernel
# runners take in parallel. Four jobs that each need Postgres and NATS are four
# copies of the same eight lines — apt, docker run, health poll, role creation —
# and a copy is a file that drifts: the NATS image was pinned in one place and
# the Postgres digest in another, and the first copy to be edited would have
# started a server the suite was not written against. So the sequence lives here,
# in one list, and each job says which units it needs.
#
# What stays in the job: the `env:` block that names the addresses the suite dials,
# and the fixtures only one job needs — the object store belongs to the job that
# runs `make check` (modules/file's S3 adapter's cases fail rather than skip
# without one), and the editor containers to the job that verifies the editor.
# modules/file's store_gate_test reads those out of the workflow text on purpose;
# moving them here would move them out of the reach of the case that checks them.
#
# Containers are started on the job's own network (the runner gives every job a
# fresh one) under the alias the endpoint in the job's env names, which is what
# the `postgres:` and `nats:` hostnames in the URLs already mean today. Each
# container's id is appended to $GITHUB_OUTPUT as `<unit>=<id>` so the job's
# always() cleanup step can remove what this one started — never by name, which
# on a shared runner would reach another job's container.
#
# CI_RESOURCES_FILE names the same output file for a rehearsal run outside a job
# (scripts/ci_setup_test.sh); CI_SETUP_PROBE makes the health waits succeed
# without a server, for the same rehearsal. Nothing else reads either variable.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
resources="${GITHUB_OUTPUT:-${CI_RESOURCES_FILE:-}}"
probe="${CI_SETUP_PROBE:-}"

# The two images the workflow pinned before this file existed, digest for digest.
# compose.yaml runs the same servers by tag, because a workstation stack follows
# the developer's own pull; a gate pins, so the server a run tested is named by
# the run.
postgres_image='postgres:16@sha256:f1c3376c26f2609ab9f29f71f824103fe2fcd8ee0346485cb6122a4f93df6f94'
nats_image='nats:2-alpine@sha256:ad7a43eb7e3337c3c38ce5d784d1461791f95f730f252d2b25eee699752a0ca3'

say() { echo "ci-setup: $*" >&2; }

if [ "$#" -eq 0 ]; then
	say "usage: $0 [postgres] [nats]; a job names the units its suite dials"
	exit 2
fi
if [ -z "$resources" ]; then
	say "no output file: GITHUB_OUTPUT is unset, so nothing could hand the ids to the cleanup step"
	exit 2
fi
network="${JOB_NETWORK:-}"
started_postgres=0
if [ -z "$network" ] && [ "$probe" != "fake" ]; then
	say "JOB_NETWORK is empty: this job has no network to attach a server to"
	exit 2
fi

# :ready waits for a unit that answers on the job's network. Every wait is on the
# server's own health endpoint, never on a sleep: the question is "may I connect",
# and the answer comes from the thing being asked. 30 seconds is the bound the
# workflow used before this file existed; the images are digest-pinned, so it is a
# pull that has already happened plus a server start.
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

# :start runs one unit and records its id under its own name, for cleanup.
start() { # start <name> <docker run arguments…>
	local name="$1"; shift
	local container
	container=$(docker run -d --network "$network" --network-alias "$name" "$@")
	printf '%s=%s\n' "$name" "$container" >>"$resources"
}

for unit in "$@"; do
	case "$unit" in
	postgres)
		started_postgres=1
		# The client arrives first: the readiness probe below is `pg_isready`, and
		# the role file below is `psql`. The job container is a plain image with no
		# database client in it, which is why the workflow installed it before the
		# service block ran; the same ordering holds here.
		if ! command -v pg_isready >/dev/null 2>&1; then
			apt-get update
			apt-get install --yes --no-install-recommends postgresql-client
		fi
		start postgres -e POSTGRES_PASSWORD=platformkit -e POSTGRES_DB=platformkit "$postgres_image"
		ready postgres pg_isready -h postgres -U postgres -d platformkit
		;;
	nats)
		start nats "$nats_image" --jetstream --http_port 8222
		ready nats curl -fsS http://nats:8222/healthz
		;;
	s3)
		say "the object store is started by the job that runs make check, where modules/file's own case reads it: see .gitea/workflows/ci.yml"
		exit 2
		;;
	*)
		say "unknown unit '$unit': this file starts postgres and nats, nothing else"
		exit 2
		;;
	esac
done

# The unprivileged role the application connection authenticates as. The file is
# idempotent (`DO $$ … IF NOT EXISTS`), so a job may run it whatever order its
# units came in. A job that started a Postgres it could not initialize has a suite
# that will fail on a missing role, so it fails here instead, with the file's name.
if [ "${started_postgres:-0}" = "1" ] && [ "$probe" != "fake" ]; then
	PGPASSWORD=platformkit psql -h postgres -U postgres -d platformkit -v ON_ERROR_STOP=1 \
		-f apps/platformkit/postgres-init.sql
fi
