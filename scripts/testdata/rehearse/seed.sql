-- Ten thousand rows in the two tables a release is most likely to change, for
-- scripts/rehearse_migrations.sh.
--
-- A migration measured against an empty table is a migration measured against
-- nothing: the index build that costs 2 ms on a development schema costs seconds
-- on the table the installation actually has, and the backfill that fits in one
-- statement on a laptop is the one this release must drain in windows. CI has no
-- production database, so the fixture is what the rehearsal has instead — the same
-- shape a dump has, at a size that makes a duration mean something.
--
-- The rows belong to the installation's oldest tenant, which is the tenant a dump
-- has too. The step connects as the owner role — a superuser under `make up` and in
-- CI — so row-level security does not bind these inserts and no session ever
-- carries a tenancy setting, which is how scripts/check_gucs.sh keeps its one
-- answer about who may write one.
--
-- Only columns every revision of these tables has are named, so the file runs
-- against the base release's schema as well as this one's: a column a later
-- migration adds is that migration's own business, and a seed that needed it would
-- be a seed that could only rehearse a release that already shipped it.

BEGIN;

WITH tenant AS (SELECT id FROM tenants ORDER BY created_at LIMIT 1)
INSERT INTO users (tenant_id, created_at, updated_at, email, display_name, status)
SELECT tenant.id,
       -- A year of history, so an index on (tenant_id, created_at DESC) has
       -- something to order and the rows are not all on one page.
       now() - make_interval(days => g % 365, hours => g % 24),
       now() - make_interval(days => g % 365, hours => g % 24),
       'rehearse' || to_hex(g) || '@rehearse.localhost',
       'Rehearsal ' || g,
       'active'
FROM generate_series(1, 10000) AS g CROSS JOIN tenant;

WITH tenant AS (SELECT id FROM tenants ORDER BY created_at LIMIT 1)
INSERT INTO audit_events (tenant_id, occurred_at, name, event_id, payload)
SELECT tenant.id,
       now() - make_interval(days => g % 365, hours => g % 24),
       'task.created',
       gen_random_uuid(),
       -- A payload that names its row the way a generated write does, because
       -- that is the shape the read path searches for.
       jsonb_build_object('id', gen_random_uuid(), 'title', 'rehearsal ' || g)
FROM generate_series(1, 10000) AS g CROSS JOIN tenant;

COMMIT;

-- The planner's own view of the copy, so a duration is the migration's cost and
-- not the cost of statistics the rehearsal made up afterwards.
ANALYZE users;
ANALYZE audit_events;
