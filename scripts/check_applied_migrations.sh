#!/usr/bin/env bash
# Refuse a change to a migration file some installation already applied:
#   check_applied_migrations.sh [BASE_COMMIT]
# kit/db checksums every applied file and stops an installation whose file
# changed after it ran, so an edit that looks like a comment fix is a boot
# failure on every database that holds the old bytes. This check judges the
# change from the merge base of BASE and HEAD to the candidate — HEAD, the index
# and the working tree, so it refuses before a commit as well as after — over
# every `migrations/*.sql` file that merge base carries: a new file passes, a
# deleted, renamed or edited one is refused, and the one modification accepted
# is a file put back, in both index and working tree, to the bytes installations
# applied. A migration upstream added after the branch point is not this change.
#
# A file's applied bytes are its blob at the oldest release tag (vMAJOR.MINOR.PATCH,
# so proof and backup tags never count) that carries it, else at the first
# first-parent commit of BASE that added it. Without release tags that rule
# cannot be read, so a repository with none is refused rather than trusted.
#
# BASE is a nonzero full commit SHA (CI passes the pull request base or the
# push's previous revision), fetched from origin when missing. Empty means the
# merge base with origin/main — never local main, which may be far behind —
# and with no origin/main, the newest release tag HEAD descends from.
# Exit 1 refuses a change; exit 2 means no base or no tags could be read.
set -euo pipefail

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

mapfile -t tags < <(git tag --list 'v*' --sort=v:refname | awk -v release="$release" '$0 ~ release')
if [ "${#tags[@]}" -eq 0 ]; then
	echo 'check-applied-migrations: no release tag in this repository; fetch the tags' >&2
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
	first="$(git log --first-parent --reverse --diff-filter=A --format=%H "$base" -- "$1" | awk 'NR == 1')"
	git rev-parse "$first:$1"
}

# Every judged path the base carries and the index or working tree differs on;
# the index diff covers what HEAD committed that the working tree left alone.
mapfile -t paths < <({
	git diff --name-only --no-renames "$base" --
	git diff --cached --name-only --no-renames "$base" --
} | awk -v scope="$scope" '$0 ~ scope' | sort -u)
if [ "${#paths[@]}" -eq 0 ] && [ "$base" = "$(git rev-parse HEAD)" ]; then
	echo 'check-applied-migrations: base is HEAD; nothing to compare'
	exit 0
fi

failed=0
for path in "${paths[@]}"; do
	git cat-file -e "$base:$path" 2>/dev/null || continue
	staged="$(git rev-parse --verify --quiet ":$path" || true)"
	worktree=''
	[ -f "$path" ] && worktree="$(git hash-object -- "$path")"
	want="$(applied "$path")"
	if [ -z "$staged" ] || [ -z "$worktree" ]; then
		echo "check-applied-migrations: $path was applied but is missing from this tree; add a new migration" >&2
		failed=1
	elif [ "$staged" = "$want" ] && [ "$worktree" = "$want" ]; then
		echo "check-applied-migrations: $path restores the bytes installations applied"
	else
		echo "check-applied-migrations: $path changed after it was applied; add a new migration" >&2
		failed=1
	fi
done

if [ "$failed" -ne 0 ]; then
	exit 1
fi
echo "check-applied-migrations: no applied migration changed since ${base:0:12}"
