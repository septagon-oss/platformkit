#!/usr/bin/env bash
# Remove the containers a CI job started, by the ids its setup step recorded.
#
# Usage: scripts/ci_cleanup.sh <container-id>…
#
# Ids, never names: the two kernel runners are shared, and a `docker rm` by name
# on a host that runs several jobs reaches whoever happens to hold that name.
# Empty arguments are skipped, because a step whose setup failed hands nothing,
# and the job's cleanup must still say whether the resources it did have went away.
set -uo pipefail

status=0
for id in "$@"; do
	if [ -z "$id" ]; then continue; fi
	if ! docker rm -f "$id"; then
		echo "ci-cleanup: could not remove $id" >&2
		status=1
	fi
done
exit "$status"
