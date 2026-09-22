#!/usr/bin/env bash
# review6_provenance_bigtree_test.sh — the seventh review's case for the deviation the
# round recorded instead of the one the sixth review suggested.
#
# scripts/review5_rehearsal_provenance_test.sh runs the rehearsal's own candidate line
# in a repository holding one committed file and one uncommitted one. That case cannot
# tell the two ways of asking "is this tree dirty" apart, because the failure the round
# measured only happens when `git status` has more output than a pipe holds: under
# `set -o pipefail`, `git status --porcelain | grep -q .` lets `grep` answer and close
# the pipe while `git` is still writing, `git` dies of SIGPIPE, pipefail takes the 141
# as the pipeline's status, and `&&` reads that as "clean" — the rehearsal then names a
# bare revision for a tree it built out of. Measured: a tree of 5000 untracked files
# prints the dirty note through the captured-text form and nothing through the pipeline
# form.
#
# So this is the same question at the size where the two answers diverge. It reads the
# line out of the step rather than restating it, and it asserts the note the release
# needs — the note the fixed line prints — never anything the broken one prints.
set -Eeuo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="$root/scripts/rehearse_migrations.sh"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
failed=0

program="$(sed -n 's/^note "candidate: \(.*\)"$/\1/p' "$script")"
[ -n "$program" ] || { echo "FAIL: no candidate line found in $script" >&2; exit 1; }

repo="$work/repo"
git init -q "$repo"
echo applied >"$repo/tracked.sql"
git -C "$repo" add tracked.sql
git -C "$repo" -c user.email=r@example -c user.name=r commit -q -m one
# More untracked lines than a pipe buffer: this is the size at which the two readings
# of "dirty" part company, and a working tree mid-refactor reaches it.
for n in $(seq 1 5000); do : >"$repo/$(printf 'filler-%04d.sql' "$n")"; done
: >"$repo/000099_new_migration.up.sql"

out="$(cd "$repo" && eval "echo \"$program\"")"
if [[ "$out" != *"dirty"* ]]; then
	echo "FAIL: the candidate line printed \"$out\" for a tree holding 5001 untracked files; whatever it asks, it is not asking whether the tree the binary was built from carries files the revision does not" >&2
	failed=1
else
	echo "ok: the candidate line names a dirty tree at the size where a piped question loses its answer: $out"
fi

# And the clean tree, so the case cannot be satisfied by a line that always says dirty.
git -C "$repo" clean -qfd
out="$(cd "$repo" && eval "echo \"$program\"")"
if [[ "$out" == *"dirty"* ]]; then
	echo "FAIL: the candidate line called a clean tree dirty: \"$out\" — a report that always warns is read as one that never does" >&2
	failed=1
else
	echo "ok: the same line leaves a clean tree named by its revision alone: $out"
fi
exit "$failed"
