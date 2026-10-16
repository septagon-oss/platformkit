#!/usr/bin/env bash
# Refuse a change to a migration file some installation already applied:
#   check_applied_migrations.sh [BASE_COMMIT]
# kit/db checksums every applied file and stops an installation whose file
# changed after it ran, so an edit that looks like a comment fix is a boot
# failure on every database that holds the old bytes. This check judges the
# change from the merge base of BASE and HEAD to the candidate — the index and
# the working tree, so it refuses before a commit as well as after — over every
# `migrations/*.sql` file that merge base carries: a new file passes, a deleted,
# renamed or edited one is refused, and the one modification accepted is a file
# put back, in both index and working tree, to the bytes installations applied.
# A migration upstream added after the branch point is not this change.
#
# The working tree is read as the bytes on disk, before any Git conversion:
# go:embed ships the file as it lies, so a line ending core.autocrlf would hide
# from `git diff` is still a changed checksum. Paths are read NUL-separated, so
# a name Git would quote is judged under its own bytes.
#
# A file's applied bytes are its blob at the oldest release tag (vMAJOR.MINOR.PATCH,
# so proof and backup tags never count) the merge base descends from and that
# carries it, else at the first first-parent commit of BASE that added it. A
# release tag outside that history — on the candidate, or on another branch — is
# not what the base's installations applied. Without such a tag that rule cannot
# be read, so the check refuses rather than trusts.
#
# BASE is a nonzero full commit SHA (CI passes the pull request base or the
# push's previous revision), fetched from origin when missing. Empty means the
# merge base with origin/main — never local main, which may be far behind —
# and with no origin/main, the newest release tag HEAD descends from.
# Exit 1 refuses a change; exit 2 means no base or no tags could be read.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

release='^v[0-9]+[.][0-9]+[.][0-9]+$'
scope='(^|/)migrations/[^/]*[.]sql$'

if [ "$#" -gt 1 ]; then
	echo 'check-applied-migrations: usage: check_applied_migrations.sh [BASE_COMMIT]' >&2
	exit 2
fi
base="${1:-}"
if [ -n "$base" ]; then
	if [[ ! "$base" =~ ^[0-9a-fA-F]{40}$ ]] || [[ "$base" =~ ^0+$ ]]; then
		echo 'check-applied-migrations: supply a nonzero full base commit SHA, or none' >&2
		exit 2
	fi
	if ! git cat-file -e "$base^{commit}" 2>/dev/null &&
		! git fetch --quiet --no-tags origin "$base" 2>/dev/null; then
		echo "check-applied-migrations: could not fetch base commit $base" >&2
		exit 2
	fi
elif git rev-parse --verify --quiet origin/main >/dev/null; then
	base="$(git merge-base HEAD origin/main)"
else
	base="$(git tag --list 'v*' --merged HEAD --sort=-v:refname | awk -v release="$release" '$0 ~ release' | awk 'NR == 1')"
	if [ -z "$base" ]; then
		echo 'check-applied-migrations: no base: pass one, fetch origin/main or fetch the release tags' >&2
		exit 2
	fi
	base="$(git rev-parse "$base^{commit}")"
fi
if ! base="$(git merge-base "$base" HEAD)"; then
	echo 'check-applied-migrations: the base shares no history with HEAD' >&2
	exit 2
fi

mapfile -t tags < <(git tag --list 'v*' --merged "$base" --sort=v:refname | awk -v release="$release" '$0 ~ release')
if [ "${#tags[@]}" -eq 0 ]; then
	echo 'check-applied-migrations: no release tag in this repository precedes the base; fetch the tags' >&2
	exit 2
fi

# applied PATH prints the blob installations hold for PATH, by the rule above.
applied() {
	local tag first
	for tag in "${tags[@]}"; do
		if git cat-file -e "$tag:$1" 2>/dev/null; then
			git rev-parse "$tag:$1"
			return
		fi
	done
	first="$(git --literal-pathspecs log --first-parent --reverse --diff-filter=A --format=%H "$base" -- "$1" | awk 'NR == 1')"
	git rev-parse "$first:$1"
}

# Every judged path the base carries, with its blob there, and the index's
# stage-0 blobs; a path in conflict has none and is read as missing.
paths=()
declare -A based=() staged=()
while IFS= read -r -d '' entry; do
	path="${entry#*$'\t'}"
	if [[ "$path" =~ $scope ]]; then
		meta="${entry%%$'\t'*}"
		paths+=("$path")
		based[$path]="${meta##* }"
	fi
done < <(git ls-tree -r -z "$base")
while IFS= read -r -d '' entry; do
	path="${entry#*$'\t'}"
	meta="${entry%%$'\t'*}"
	if [[ "$path" =~ $scope ]] && [ "${meta##* }" = 0 ]; then
		meta="${meta% *}"
		staged[$path]="${meta##* }"
	fi
done < <(git ls-files -s -z)

failed=0
judged=0
for path in "${paths[@]}"; do
	index="${staged[$path]:-}"
	worktree=''
	[ -f "$path" ] && worktree="$(git hash-object --no-filters -- "$path")"
	if [ "$index" = "${based[$path]}" ] && [ "$worktree" = "${based[$path]}" ]; then
		continue
	fi
	judged=1
	want="$(applied "$path")"
	if [ -z "$index" ] || [ -z "$worktree" ]; then
		echo "check-applied-migrations: $path was applied but is missing from this tree; add a new migration" >&2
		failed=1
	elif [ "$index" = "$want" ] && [ "$worktree" = "$want" ]; then
		echo "check-applied-migrations: $path restores the bytes installations applied"
	else
		echo "check-applied-migrations: $path changed after it was applied; add a new migration" >&2
		failed=1
	fi
done

if [ "$failed" -ne 0 ]; then
	exit 1
fi
if [ "$judged" -eq 0 ] && [ "$base" = "$(git rev-parse HEAD)" ]; then
	echo 'check-applied-migrations: base is HEAD; nothing to compare'
	exit 0
fi
echo "check-applied-migrations: no applied migration changed since ${base:0:12}"
