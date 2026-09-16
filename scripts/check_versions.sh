#!/usr/bin/env bash
# Refuse local version overrides in a repository's Go module:
#   check_versions.sh [repository]
# A `replace` directive in go.mod and a go.work file both make a build use a
# checkout instead of the versions the module declares, so a check that passed
# with one in place says nothing about the versions anyone else resolves. The
# foundation owns this rule; a consumer runs the script from its resolved
# foundation dependency and supplies its own repository directory, as with
# check_imports.sh. Comments in go.mod are ignored; a directive is not.
set -euo pipefail

root="$(cd "${1:-$(dirname "${BASH_SOURCE[0]}")/..}" && pwd)"
if [ ! -f "$root/go.mod" ]; then
	echo "versions: $root has no go.mod" >&2
	exit 2
fi

failed=0
# Both forms: a one-line `replace a => b` and lines inside a `replace (` block,
# which carry the arrow without the keyword. Trailing comments are stripped
# first so a commented-out directive does not fail the check.
replaced="$(sed 's#//.*$##' "$root/go.mod" | awk '
	/^[[:space:]]*replace([[:space:]]|$)/ { print NR ": " $0; next }
	/=>/ { print NR ": " $0 }
')"
if [ -n "$replaced" ]; then
	printf '%s\n' "$replaced" >&2
	echo "OUT OF BOUNDS: go.mod replaces a dependency; declare the version the build must resolve." >&2
	failed=1
fi
if [ -e "$root/go.work" ]; then
	echo "OUT OF BOUNDS: $root/go.work selects local checkouts; the module's declared versions are the build." >&2
	failed=1
fi
if [ "$failed" -ne 0 ]; then
	exit 1
fi
echo "versions: go.mod declares every dependency and no go.work is present"
