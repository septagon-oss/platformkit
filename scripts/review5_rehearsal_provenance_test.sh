#!/usr/bin/env bash
# review5_rehearsal_provenance_test.sh — the sixth review's case for the line the
# rehearsal report is headed with.
#
# migrations/README.md makes `make rehearse` "the step a release requires before a
# version is published", and the report it prints is the artefact a release reads.
# Its first line names the candidate:
#
#	note "candidate: $(git rev-parse --short HEAD)$(git diff --quiet || echo ' with a dirty tree')"
#
# `git diff` compares tracked content only. A migration file that is not yet
# committed — which is exactly the file an author writes and then rehearses, and
# exactly the file `go build` picks up into the binary the step then runs — is
# invisible to it, so the report of a run that applied that file is headed with a
# revision that does not contain it. `git status --porcelain` is the question.
#
# This runs the step's own line, read out of the step rather than restated, in a
# throwaway repository holding one committed file and one uncommitted one.
set -Eeuo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="$root/scripts/rehearse_migrations.sh"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
failed=0

program="$(sed -n 's/^note "candidate: \(.*\)"$/\1/p' "$script")"
[ -n "$program" ] || { echo "FAIL: no candidate line found in $script" >&2; exit 1; }

git init -q "$work/repo"
echo applied >"$work/repo/tracked.sql"
git -C "$work/repo" add tracked.sql
git -C "$work/repo" -c user.email=r@example -c user.name=r commit -q -m one
echo pending >"$work/repo/000099_new_migration.up.sql" # never added: what an author rehearses

cd "$work/repo"
if ! out="$(eval "echo \"$program\"")"; then
	echo "FAIL: the step's candidate line does not run" >&2
	exit 1
fi
if [[ "$out" != *"dirty"* ]]; then
	echo "FAIL: the candidate line printed \"$out\" for a tree holding an uncommitted migration file; it names a revision that does not contain the file the run applied" >&2
	failed=1
else
	echo "ok: the candidate line names the tree the run built from: $out"
fi
exit "$failed"
