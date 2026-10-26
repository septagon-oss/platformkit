#!/usr/bin/env bash
# What a disposable installation holds, as one sorted list of facts — the evidence
# scripts/rehearse_migrations.sh compares before an upgrade with after it.
#
#	rehearse_facts.sh --admin-url URI --database NAME [--app-url URI] [--skip-boundary]
#
# Each line is a key and the numbers that answer it, tab-separated:
#
#	table/tenant        rows, digest      one line per seeded table per tenant, the
#	                                    digest taken over the columns a release may
#	                                    not change — a body's text, a file's sha256
#	                                    and storage key, an event's payload — so
#	                                    "the data survived" is a line that can
#	                                    differ and not a feeling
#	outbox/tenant       rows, pending, digest      pending is published_at IS NULL:
#	                                    the events that have not left the copy yet
#	ledger              rows, digest      the applied history, row by row
#	rls                 forced, policies  how many tenant tables still force their
#	                                    policy: the authority half of the comparison
#	other/tenant/table  visible, foreign  what a session scoped to that tenant may
#	                                    read of one table, and how much of it belongs
#	                                    to another: the boundary's own verdict, and
#	                                    the only zero in this list that must stay one
#
# The digests are scoped to this rehearsal's rows by the markers
# scripts/testdata/rehearse/seed.sql and populated.sql write, so a release that
# legitimately adds rows of its own is not reported as data loss, while a changed
# body, a rehashed file or a dropped event is. Rows the fixtures did not write are
# not this script's, and no target is asked to leave them alone.
#
# The boundary lines read through the *application* role — the unprivileged one row-
# level security binds — because the owner role this step migrates as answers every
# such question with "everything". The tenancy setting arrives as a connection
# parameter (PGOPTIONS), the way a client's DSN supplies it: this script never writes
# the setting, it reads under one, and sees one tenant and no other.
#
# Exit codes:
#
#	0  every fact asked for was answered
#	1  the database holds no seeded facts to take
#	2  it could not run: no URI, no client, no such database
set -euo pipefail

facts_out="$(mktemp)"
cleanup() { rm -f "$facts_out"; }
trap cleanup EXIT

admin_url=""
app_url=""
database=""
boundary=1
while [ $# -gt 0 ]; do
	case "$1" in
	--admin-url) [ $# -ge 2 ] || exit 2; admin_url="$2"; shift 2 ;;
	--app-url) [ $# -ge 2 ] || exit 2; app_url="$2"; shift 2 ;;
	--database) [ $# -ge 2 ] || exit 2; database="$2"; shift 2 ;;
	--skip-boundary) boundary=0; shift ;;
	*) echo "rehearse-facts: unknown argument $1" >&2; exit 2 ;;
	esac
done
[ -n "$admin_url" ] && [ -n "$database" ] || {
	echo "rehearse-facts: --admin-url and --database are required" >&2
	exit 2
}
command -v psql >/dev/null || { echo "rehearse-facts: no psql to ask the database with" >&2; exit 2; }
case "$admin_url" in *://*) ;; *) echo "rehearse-facts: --admin-url is not a postgres URI" >&2; exit 2 ;; esac
database="${database//[!A-Za-z0-9_]/}"
[ -n "$database" ] || { echo "rehearse-facts: --database names no database" >&2; exit 2; }

uri_with_database() {
	local rest="${1#*://}" authority tail="" query=""
	if [[ "$rest" == */* ]]; then authority="${rest%%/*}"; tail="${rest#*/}"; else authority="$rest"; fi
	[[ "$tail" == *\?* ]] && query="?${tail#*\?}"
	printf '%s://%s/%s%s' "${1%%://*}" "$authority" "$2" "$query"
}
owner_uri="$(uri_with_database "$admin_url" "$database")"

# One question per fact, each appended to the same file and sorted once at the end, so
# the caller compares two lists in the same order whatever order they were asked in.
ask_line() { printf '%s\t%s\n' "$1" "$(psql "$owner_uri" -v ON_ERROR_STOP=1 -tA -c "$2")" >>"$facts_out"; }

# Each table digests the columns whose change would be the finding: text for content,
# the hash and the key for a file, the payload for an event, the state for a task.
facts_sql="
WITH seeded AS (
	SELECT 'users' AS table_name, u.tenant_id, u.id || ':' || u.email || ':' || u.display_name || ':' || u.status AS fact
	FROM users u WHERE u.email LIKE 'rehearse%@rehearse.localhost'
	UNION ALL
	SELECT 'contents', c.tenant_id, c.id || ':' || c.slug || ':' || c.title || ':' || md5(c.body) || ':' || c.status
	FROM contents c WHERE c.slug LIKE 'rehearse-%'
	UNION ALL
	SELECT 'files', f.tenant_id, f.id || ':' || f.name || ':' || f.sha256 || ':' || f.storage_key || ':' || f.size || ':' || f.visibility
	FROM files f WHERE f.name LIKE 'rehearse-file-%'
	UNION ALL
	SELECT 'tasks', k.tenant_id, k.id || ':' || k.title || ':' || k.status || ':' || k.priority || ':' || k.source_ref
	FROM tasks k WHERE k.title LIKE 'Rehearsal task %'
	UNION ALL
	SELECT 'platformkit_outbox', o.tenant_id, o.id || ':' || o.name || ':' || md5(o.payload::text)
	FROM platformkit_outbox o WHERE o.name LIKE 'rehearse.probe.%'
	UNION ALL
	SELECT 'audit_events', a.tenant_id, a.event_id || '=' || octet_length(a.payload::text)
	FROM audit_events a WHERE a.name = 'task.created'
)
SELECT s.table_name || '/' || t.slug,
	count(*)::text,
	coalesce(md5(string_agg(s.fact, ',' ORDER BY s.fact)), '-')
FROM seeded s JOIN tenants t ON t.id = s.tenant_id
GROUP BY s.table_name, t.slug"

if ! psql "$owner_uri" -v ON_ERROR_STOP=1 -F $'\t' -tA -c "$facts_sql" >>"$facts_out"; then
	echo "rehearse-facts: $database holds no seeded facts to read, so no upgrade of it can be measured" >&2
	exit 1
fi

ask_line ledger "
SELECT count(*) || chr(9) || coalesce(md5(string_agg(owner || '/' || version || '=' || name || '=' || checksum ||
	'=' || coalesce(applied_at::text, ''), ',' ORDER BY owner, version)), '-')
FROM schema_migrations"
ask_line rls "
SELECT (SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema() AND c.relkind = 'r' AND c.relforcerowsecurity
			AND EXISTS (SELECT 1 FROM pg_attribute a
				WHERE a.attrelid = c.oid AND a.attname = 'tenant_id' AND a.attnum > 0)) || chr(9) ||
	(SELECT count(*) FROM pg_policies WHERE schemaname = current_schema() AND policyname LIKE '%tenant%')"
ask_line tenants "
SELECT count(*) || chr(9) ||
	coalesce(md5(string_agg(slug || '=' || status || '=' || name, ',' ORDER BY slug)), '-')
FROM tenants"
ask_line outbox.pending "SELECT count(*) FROM platformkit_outbox WHERE published_at IS NULL"

# The boundary, read as the application role. Each tenant's own session is asked what
# it can see and, in the same breath, how much of it belongs to somebody else: the
# second number is the one a release can break, and zero is its only pass.
if [ "$boundary" = 1 ] && [ -n "$app_url" ]; then
	while IFS=$'\t' read -r slug id; do
		[[ "$id" =~ ^[0-9a-fA-F-]{36}$ ]] || {
			echo "rehearse-facts: tenant '$slug' carries an id this script will not put in a query: $id" >&2
			exit 2
		}
		boundary_sql="
SELECT 'other/' || '$slug' || '/' || v.table_name || chr(9) || v.visible::text || chr(9) || v.other::text
FROM (
	SELECT 'users' AS table_name, count(*) AS visible, count(*) FILTER (WHERE u.tenant_id <> '$id'::uuid) AS other
	FROM users u
	UNION ALL
	SELECT 'contents', count(*), count(*) FILTER (WHERE c.tenant_id <> '$id'::uuid) FROM contents c
	UNION ALL
	SELECT 'files', count(*), count(*) FILTER (WHERE f.tenant_id <> '$id'::uuid) FROM files f
	UNION ALL
	SELECT 'tasks', count(*), count(*) FILTER (WHERE k.tenant_id <> '$id'::uuid) FROM tasks k
	UNION ALL
	SELECT 'platformkit_outbox', count(*), count(*) FILTER (WHERE o.tenant_id <> '$id'::uuid) FROM platformkit_outbox o
	UNION ALL
	SELECT 'audit_events', count(*), count(*) FILTER (WHERE a.tenant_id <> '$id'::uuid) FROM audit_events a
) AS v"
		if ! PGOPTIONS="-c platformkit.tenant_id=$id" \
			psql "$(uri_with_database "$app_url" "$database")" -v ON_ERROR_STOP=1 -F $'\t' -tA \
			-c "$boundary_sql" >>"$facts_out"; then
			echo "rehearse-facts: the application role could not answer for tenant $slug in $database" >&2
			exit 2
		fi
	done < <(psql "$owner_uri" -v ON_ERROR_STOP=1 -F $'\t' -tA -c "SELECT slug, id FROM tenants ORDER BY slug")
fi

sort "$facts_out"
