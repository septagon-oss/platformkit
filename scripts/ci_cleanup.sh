#!/usr/bin/env bash
# Remove the containers a CI job started, by the handles that name them.
#
# Usage: scripts/ci_cleanup.sh <container-handle>…
#
# A handle is either the id its own step recorded or the name the job gave the
# container. Both are passed, and they reach the same container: main learned on run
# 245 that the id is the handle a job can lose — a step its own deadline cuts off
# never finishes writing $GITHUB_OUTPUT — and a container still running is what
# makes `docker image rm` refuse and the job's network unremovable.
#
# A name is safe only because it cannot reach anyone but this job: the two kernel
# runners are shared, and `docker rm` on a bare `nats` would reach whichever job
# happens to hold that name. So every name carries the run id and the unit, and the
# job between them wherever two jobs of one run start the same unit — the broker
# three of the four jobs starts, the shared store two of them do. Empty arguments
# are skipped, because the step that would have produced that handle may never have
# run, and so is a handle that names nothing on this daemon: there is nothing left
# to remove.
set -uo pipefail

status=0
for handle in "$@"; do
	if [ -z "$handle" ]; then continue; fi
	if ! docker inspect "$handle" >/dev/null 2>&1; then continue; fi
	if ! docker rm -f "$handle"; then
		echo "ci-cleanup: could not remove $handle" >&2
		status=1
	fi
done
exit "$status"
