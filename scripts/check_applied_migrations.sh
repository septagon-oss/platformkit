#!/usr/bin/env bash
# Refuse a change to a migration file some installation already applied:
#   check_applied_migrations.sh [BASE_COMMIT]
# kit/db checksums every applied file and stops an installation whose file
# changed after it ran, so an edit that looks like a comment fix is a boot
# failure on every database that holds the old bytes. This check judges the
# change from BASE to HEAD over every `migrations/*.sql` file the base carries:
# a new file passes, a deleted, renamed or edited one is refused, and the one
# modification accepted is a file put back to the bytes installations applied.
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

mapfile -t tags < <(git tag --list 'v*' --sort=v:refname | awk -v release="$release" '$0 ~ release')
if [ "${#tags[@]}" -eq 0 ]; then
	echo 'check-applied-migrations: no release tag in this repository; fetch the tags' >&2
	exit 2
fi

if [ "$(git rev-parse "$base^{commit}")" = "$(git rev-parse HEAD)" ]; then
	echo 'check-applied-migrations: base is HEAD; nothing to compare'
	exit 0
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

failed=0
while IFS=$'\t' read -r status path; do
	case "$status" in
	A) continue ;;
	D)
		echo "check-applied-migrations: $path was applied but is missing from this tree; add a new migration" >&2
		failed=1
		;;
	*)
		if [ "$(git rev-parse "HEAD:$path")" = "$(applied "$path")" ]; then
			echo "check-applied-migrations: $path restores the bytes installations applied"
		else
			echo "check-applied-migrations: $path changed after it was applied; add a new migration" >&2
			failed=1
		fi
		;;
	esac
done < <(git diff --name-status --no-renames "$base" HEAD | awk -F '\t' -v scope="$scope" '$2 ~ scope')

if [ "$failed" -ne 0 ]; then
	exit 1
fi
echo "check-applied-migrations: no applied migration changed since ${base:0:12}"
