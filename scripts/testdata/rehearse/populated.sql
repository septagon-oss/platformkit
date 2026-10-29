-- The facts a release has to carry across: a second tenant, its own users, page
-- text, file records and events still queued — for scripts/rehearse_migrations.sh.
--
-- scripts/testdata/rehearse/seed.sql measures the *cost*: ten thousand rows so a
-- duration means something. This file measures the *promise*: that an installation
-- with more than one tenant, with body text a person wrote, with file rows whose
-- bytes live outside the database, and with events that have not left the outbox,
-- is the same installation after the upgrade. A rehearsal of an empty schema proves
-- the DDL runs; it says nothing about whether what was in the old tables is still in
-- the new ones, and it cannot see a tenant boundary break.
--
-- Two rules make this file runnable against the base release's schema and not only
-- this one's:
--
--   * it names no table or column a supported source's own ledger does not already
--     hold — a file_uses row or an operation row is created by the release that
--     introduces them, never faked here, so what survives the upgrade is a fact the
--     old installation really had;
--   * every row it writes is reachable by the marker scripts/rehearse_facts.sh
--     selects on, so the comparison after the upgrade reads this fixture's rows and
--     not whatever a migration legitimately added of its own.
--
-- The second tenant holds a host this rehearsal declares, which is what lets
-- 000043 place it: a tenant with no host, or one whose host the boot never named,
-- is that migration's refusal to guess, and this fixture is here to be carried, not
-- to be refused. Like seed.sql, this runs as the owner role, whose inserts are not
-- what the check is about — the read-side boundary is observed separately, as the
-- application role, by scripts/rehearse_facts.sh.

BEGIN;

-- The installation's second tenant, at the host the candidate declares alongside
-- the first. Status active: a suspended tenant is a routing decision, not a data
-- shape, and nothing here depends on it.
INSERT INTO tenants (slug, name, status, created_at, updated_at)
VALUES ('rehearse-two', 'Rehearsal Two', 'active', now() - interval '200 days', now() - interval '200 days');

INSERT INTO tenant_hosts (host, tenant_id, is_primary)
SELECT 'rehearse-two.localhost', t.id, true
FROM tenants AS t
WHERE t.slug = 'rehearse-two';

-- One user per tenant per hundred, so a digest that names a tenant is answered by
-- that tenant's rows. The first tenant keeps the emails seed.sql wrote for its
-- volume rows; this file adds the ones the boundary check reads.
INSERT INTO users (tenant_id, created_at, updated_at, email, display_name, status)
SELECT t.id,
       now() - make_interval(days => g % 200),
       now() - make_interval(days => g % 200),
       'rehearsepop' || t.slug || g || '@rehearse.localhost',
       'Rehearsal person ' || t.slug || ' ' || g,
       CASE WHEN g % 7 = 0 THEN 'suspended' ELSE 'active' END
FROM tenants AS t
CROSS JOIN generate_series(1, 100) AS g;

-- File records first, because the page bodies reference them. The sha256 is the
-- hash the upload measured, so it is a value the database never recomputes; the
-- storage key is the only door to the bytes. Both have to arrive unchanged.
INSERT INTO files (tenant_id, created_at, updated_at, name, content_type, size, sha256,
                   storage_key, visibility, uploader_id)
SELECT t.id,
       now() - make_interval(days => g),
       now() - make_interval(days => g),
       'rehearse-file-' || g || '.png',
       'image/png',
       (g * 1023)::bigint,
       -- Sixty-four hex characters, from the tenant and the row alone: the column is
       -- what an upload measured on the way to storage, and nothing recomputes it, so
       -- the fixture only has to be a value that cannot arrive as anything else.
       md5(t.id::text || ':blob:' || g) || md5(g || ':blob:' || t.id::text),
       'rehearse/' || md5(t.id::text || '/blob/' || g),
       CASE WHEN g % 3 = 0 THEN 'public' ELSE 'private' END,
       (SELECT u.id FROM users AS u WHERE u.tenant_id = t.id ORDER BY u.id LIMIT 1)
FROM tenants AS t
CROSS JOIN generate_series(1, 60) AS g;

-- Page text: Markdown as it was written, a reference to a file of the same tenant,
-- and a non-ASCII run, because the thing at risk here is a byte of what a person
-- typed. Two thirds published, so the published-path index has rows behind it.
INSERT INTO contents (tenant_id, created_at, updated_at, slug, title, body, kind, status,
                      published_at, author_id)
SELECT f.tenant_id,
       now() - make_interval(days => f.g),
       now() - make_interval(days => f.g),
       'rehearse-' || f.tenant_id || '-' || f.g,
       'Rehearsal page ' || f.g,
       '## Rehearsal ' || f.g || E'\n\nThe body names a file: [blob](/api/v1/file/public/' ||
           f.id || E') and carries a naïve café § — the bytes a person typed.\n',
       'page',
       CASE WHEN f.g % 3 = 0 THEN 'draft' ELSE 'published' END,
       CASE WHEN f.g % 3 = 0 THEN NULL ELSE now() - make_interval(days => f.g) END,
       (SELECT u.id FROM users AS u WHERE u.tenant_id = f.tenant_id ORDER BY u.id LIMIT 1)
FROM (
	SELECT files.tenant_id, files.id,
		(split_part(split_part(files.name, '-', 3), '.', 1))::int AS g
	FROM files
	WHERE files.name LIKE 'rehearse-file-%'
) AS f;

-- Tasks, the module this repository's own reference work item is, one queue per
-- tenant across the states the SLA sweep reads.
INSERT INTO tasks (tenant_id, created_at, updated_at, title, description, status, priority,
                   source, source_ref, assignee_id, due_at, sla_deadline, resolved_at)
SELECT t.id,
       now() - make_interval(days => g),
       now() - make_interval(days => g),
       'Rehearsal task ' || t.slug || ' ' || g,
       'Carried across the upgrade for tenant ' || t.slug,
       CASE g % 4 WHEN 0 THEN 'open' WHEN 1 THEN 'in_progress' WHEN 2 THEN 'done' ELSE 'cancelled' END,
       CASE g % 3 WHEN 0 THEN 'urgent' WHEN 1 THEN 'high' ELSE 'normal' END,
       'rehearse',
       'rehearse/' || t.slug || '/' || g,
       (SELECT u.id FROM users AS u WHERE u.tenant_id = t.id ORDER BY u.id LIMIT 1),
       now() + make_interval(days => g % 14),
       now() + make_interval(days => g % 14),
       CASE WHEN g % 4 IN (2, 3) THEN now() - make_interval(days => g) END
FROM tenants AS t
CROSS JOIN generate_series(1, 80) AS g;

-- The outbox as an installation actually holds it: some events the relay has taken,
-- more it has not. A pending row is a promise a release may not lose — the relay
-- publishes what it can read, and an event that stops being readable is an event
-- nobody will ever be told about. The payload names its own row, so a payload that
-- arrived as something else changes the digest.
INSERT INTO platformkit_outbox (tenant_id, name, payload, created_at, published_at)
SELECT t.id,
       'rehearse.probe.' || (g % 5),
       jsonb_build_object('seq', g, 'slug', t.slug, 'title', 'Rehearsal event ' || t.slug || ' ' || g),
       now() - make_interval(hours => g),
       CASE WHEN g % 4 = 0 THEN now() - make_interval(hours => g) END
FROM tenants AS t
CROSS JOIN generate_series(1, 60) AS g;

COMMIT;

-- The planner sees the fixture, so a duration stays the migration's cost.
ANALYZE users;
ANALYZE contents;
ANALYZE files;
ANALYZE tasks;
ANALYZE platformkit_outbox;
