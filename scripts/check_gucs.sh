#!/usr/bin/env bash
# Fails when anything outside kit/db writes one of the tenancy settings, and
# when any Go file outside kit/db names one at all.
#
# platformkit.tenant_id and platformkit.system_access are placeholder GUCs, and
# Postgres classes placeholders USERSET: every role may set them, and no
# privilege can be withheld. So the boundary is not a database permission. It is
# three things together:
#
#   1. db.Tx[db.Tenant] and db.Tx[db.System] are different types, so crossing
#      the tenant by accident does not compile;
#   2. this grep, so crossing it deliberately cannot be done in plain sight;
#   3. the re-read in db.Run and db.Pending.Close, which catches the naive
#      escape: code that sets a setting and does not put it back rolls back
#      instead of committing. It catches nothing else — an escape that restores
#      the value before returning re-reads clean — so this grep, and not the
#      re-read, is the control for a deliberate one.
#
# What the database enforces on its own is the forgotten predicate: a query with
# no WHERE tenant_id returns this tenant's rows and no one else's. That is the
# claim, and it is the one worth having.
#
# The shapes this looks for, and why each one is on the list:
#
#   set_config( 'platformkit…   — the documented door, any spacing, and the
#                                 qualified pg_catalog.set_config alike
#   set_config( $1 …            — the name supplied as a parameter, caught by
#                                 the Go rule below instead: to pass a name it
#                                 has to be a literal somewhere
#   set_config( 'platformkit' || '.tenant_id'  — assembled at runtime
#   SET LOCAL platformkit.…     — the transaction-local write
#   SET platformkit.…           — WITHOUT local, the dangerous one: it survives
#                                 the commit and stays on the pooled connection,
#                                 so it never passes the re-read
#   ALTER ROLE / DATABASE / SYSTEM … SET platformkit.…
#                               — a persistent default for every connection the
#                                 pool will ever open
#
# .sql is scanned for those writes because a migration is exactly as authoritative
# as a .go file and was, until here, invisible to this gate. Reads stay allowed
# there: migrations/000001 defines the policies and must current_setting() the
# value it is written to match. A .go file may not even name the settings, since
# db.Run, db.RunSystem and db.TenantOf are how Go says whose transaction this is.
#
# What this cannot see is a name assembled from pieces at runtime — a key out of
# a map, a string built across three functions. That is not an accident, and the
# answer is not a cleverer grep: it is a reviewer reading a set_config call and
# asking why a module is writing a kernel setting. The re-read and the type
# parameter stay underneath it, and neither one alone is the control.
set -euo pipefail

# A consumer supplies its repository; the exemption belongs to the foundation
# module identity, not to any directory a consumer happens to call kit/db.
foundation="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
root="$(cd "${1:-$foundation}" && pwd)"
foundation_module="$(sed -n 's/^module[[:space:]]*//p' "$foundation/go.mod")"
target_module="$(sed -n 's/^module[[:space:]]*//p' "$root/go.mod")"
[ -n "$target_module" ] || { echo "gucs: missing module declaration" >&2; exit 2; }
cd "$root"
git rev-parse --show-toplevel >/dev/null
paths=('*.go' '*.sql')
if [ "$target_module" = "$foundation_module" ]; then
    paths+=(':(exclude,glob)kit/db/*.go')
fi

# LIT is one of the four ways an argument starts when it is a value rather than
# an elision: a quote of either kind, a backtick, or a bind parameter. It is
# spelled \047 because the whole program sits in single quotes here.
program='
BEGIN {
	LIT  = "[\"\047`$]"
	RE_SC       = "set_config[ \t]*\\([ \t]*" LIT
	RE_SC_NOUNS = "platformkit|tenant_id|system_access"
	RE_SET      = "(^|[^a-z_])set[ \t]+(local[ \t]+|session[ \t]+)?" LIT "?platformkit\\."
	RE_ALTER    = "alter[ \t]+(role|database|system)"
	RE_ALTER_SET = "set[ \t]+" LIT "?platformkit\\."
	RE_NAME     = "platformkit\\.(tenant_id|system_access)"
}
{
	line = tolower($0)
	write = 0
	if (line ~ RE_SC && line ~ RE_SC_NOUNS) write = 1
	if (line ~ RE_SET) write = 1
	if (line ~ RE_ALTER && line ~ RE_ALTER_SET) write = 1
	if (golang && line ~ RE_NAME) write = 1
	if (write) printf "%s:%d: %s\n", file, FNR, $0
}'

hits=""
# git ls-files sees new files too, the same way tools/locbudget does. A file it
# still tracks but the working tree has deleted is skipped, which is the state
# between a `rm` and its commit.
while IFS= read -r -d '' file; do
	[ -f "$file" ] || continue
	case "$file" in *.go) golang=1 ;; *) golang=0 ;; esac
	if found="$(awk -v file="$file" -v golang="$golang" "$program" "$file")"; then
		[ -z "$found" ] || hits="$hits$found"$'\n'
	fi
# The exclusion is kit/db's own files and not its subdirectories: kit/db/dbtest
# opens owner connections for tests and has no more business writing a tenancy
# setting than any other package.
done < <(git ls-files -z -c -o --exclude-standard -- "${paths[@]}")

if [ -n "$hits" ]; then
	printf '%s' "$hits" >&2
	echo "" >&2
	echo "OUT OF BOUNDS: only kit/db may write a platformkit.* setting, and no Go" >&2
	echo "file outside it may name one. Use db.Run, db.RunSystem or db.TenantOf;" >&2
	echo "see docs/adr/0003-tenancy-by-postgres.md." >&2
	exit 1
fi

echo "gucs: only the foundation kit/db writes the tenancy settings, in Go or in SQL"
