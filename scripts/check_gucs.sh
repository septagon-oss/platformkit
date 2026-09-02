#!/usr/bin/env bash
# Fails when any Go file outside kit/db writes one of the tenancy settings.
#
# platformkit.tenant_id and platformkit.system_access are placeholder GUCs, and
# Postgres classes placeholders USERSET: every role may set them, and no
# privilege can be withheld. So the boundary is not a database permission. It is
# three things together:
#
#   1. db.Tx[db.Tenant] and db.Tx[db.System] are different types, so crossing
#      the tenant by accident does not compile;
#   2. this grep, so crossing it deliberately cannot be done quietly;
#   3. the re-read in db.Run and db.Pending.Close, so a transaction that crossed
#      it anyway rolls back instead of committing.
#
# What the database enforces on its own is the forgotten predicate: a query with
# no WHERE tenant_id returns this tenant's rows and no one else's. That is the
# claim, and it is the one worth having.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

hits=""
# git ls-files sees new files too, the same way tools/locbudget does. A file it
# still tracks but the working tree has deleted is skipped, which is the state
# between a `rm` and its commit.
while IFS= read -r file; do
	[ -f "$file" ] || continue
	if found="$(grep -Hn -e "set_config('platformkit\." -e "SET LOCAL platformkit\." "$file")"; then
		hits="$hits$found"$'\n'
	fi
done < <(git ls-files -c -o --exclude-standard -- '*.go' ':!:kit/db/*')

if [ -n "$hits" ]; then
	printf '%s' "$hits" >&2
	echo "" >&2
	echo "OUT OF BOUNDS: only kit/db may write a platformkit.* setting." >&2
	echo "Use db.Run or db.RunSystem; see docs/adr/0003-tenancy-by-postgres.md." >&2
	exit 1
fi

echo "gucs: only kit/db writes the tenancy settings"
