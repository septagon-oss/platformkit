#!/usr/bin/env bash
# Fetch the apt package index, retrying the failures that say something about the mirror.
#
# This file is what stands between a half-synced Ubuntu mirror and a red pull request. Run 56143, job
# 56770 (2026-10-09, head cea7e46b) lost its `check` job at
# `Install the database client, the socket probe and the YAML reader` with `##[error]Process completed
# with exit code 100` and no assertion anywhere in the log. The whole of what the step said was:
#
#   E: Failed to fetch http://security.ubuntu.com/ubuntu/dists/noble-security/main/binary-amd64/Packages.gz
#      File has unexpected size (1377452 != 1380818). Mirror sync in progress? [IP: 91.189.91.82 80]
#   E: Some index files failed to download. They have been ignored, or old ones used instead.
#
# which is Canonical's mirror serving a `Release` from 08:43Z beside a `Packages.gz` it had already
# rolled forward — a fact about the CDN node that happened to answer, and nothing whatever about the
# tree. The step ran under `bash -e {0}`, so the exit ended it before `apt-get install`, the three
# tools went uninstalled, and every guard that asks one of them would have refused for that reason
# rather than this sentence being read as what it is.
#
# So a fetch that reads as the mirror not answering is tried again after a wait. The bound is the
# point of this file, and it is pinned by scripts/ci_apt_index_test.sh: giving up on the first blip is
# the shape that was red, and retrying forever is a hang nobody reads as a hang, so the loop tries
# `1 + ${#apt_index_waits}` times at most and then refuses.
#
# What is NOT retried: anything that is a fact about the index rather than the route to it. `E: Unable
# to locate package`, `E: Unmet dependencies`, a held broken package, a source list with no Release
# file — those say the tree or the image asks for something that is not there, and a retry buys an
# minute before the same sentence. The markers below are the ways apt says "the mirror would not
# answer", the first three taken word for word from the refusal above.
#
# Two limits of this shape, both accepted:
#   - A mirror sync that outlasts the waits still ends this step, and the pull request stays red. The
#     cure for that is a pinned apt mirror or a baked toolchain image, which is a larger decision than
#     a retry; what this buys is that a blip and a two-second resync cost the job 40 seconds.
#   - The tries are sequential and this step sits at the front of the job, so the wait is dead time on
#     the critical path. It is bounded at 40 s against a job ceiling of 75 minutes.
#
# Sourcing this file defines apt_index_waits, the transient markers and retry_wait without running
# anything, which is how the cases ask the classifier about each failure without a mirror to break.
set -euo pipefail

# The wait, in seconds, before the second and third try. Two waits sit out the resync of one index
# file — the shape measured above, where the `Release` had been published an hour and forty minutes
# before the run asked — and a third try is what makes it a bound rather than a hope.
apt_index_waits=(10 30)

# The cases shorten this list so the bound can be run rather than waited for; one case pays the real
# seconds, so the numbers above stay pinned by behaviour and not by a comment.
if [ -n "${PKIT_APT_INDEX_WAITS:-}" ]; then
	read -r -a apt_index_waits <<<"$PKIT_APT_INDEX_WAITS"
fi

# How apt says the mirror did not answer. Lower-cased output is matched against these.
apt_index_transient=(
	"mirror sync in progress"
	"failed to fetch"
	"some index files failed to download"
	"hash sum mismatch"
	"could not resolve host"
	"temporary failure resolving"
	"name or service not known"
	"unable to connect"
	"connection refused"
	"connection timed out"
	"connection reset"
	"network is unreachable"
	"socket write error"
	"received unexpected file size"
	"bad gateway"
	"service unavailable"
	"gateway time-out"
	"unexpected eof"
)

retry_wait() { # <tries already spent> <what apt wrote> — prints the seconds to wait, prints nothing and returns 1 when the failure is final
	local after="$1" lowered="${2,,}" marker
	for marker in "${apt_index_transient[@]}"; do
		case "$lowered" in
		*"$marker"*)
			if [ "$after" -lt "${#apt_index_waits[@]}" ]; then
				printf '%s\n' "${apt_index_waits[$after]}"
				return 0
			fi
			return 1
			;;
		esac
	done
	return 1
}

apt_index_main() {
	local try=1 output="" status=0 pause="" plural
	while true; do
		status=0
		output="$(apt-get update 2>&1)" || status=$?
		if [ "$status" -eq 0 ]; then
			if [ -n "$output" ]; then
				printf '%s\n' "$output"
			fi
			if [ "$try" -gt 1 ]; then
				plural=y
				[ "$try" -eq 2 ] || plural=ies
				echo "apt index: fetched on try $try, after $((try - 1)) retr$plural"
			fi
			return 0
		fi
		if ! pause="$(retry_wait "$((try - 1))" "$output")"; then
			printf '%s\n' "$output" >&2
			echo "apt index: refused on try $try — apt wrote the above, and this step asks apt-get update at most $((1 + ${#apt_index_waits[@]})) times in all, so what is left is a failure no wait outlasts" >&2
			return 1
		fi
		printf '%s\n' "$output"
		echo "+ retry $try waiting ${pause}s for the mirror: ${output##*$'\n'}" >&2
		sleep "$pause"
		try=$((try + 1))
	done
}

if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
	apt_index_main "$@"
fi
