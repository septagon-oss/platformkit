-- pkit: allow=index-not-concurrent reason=the index covers only rows where operator is true, one row per app, and CONCURRENTLY would have to live in its own autocommit file, where it could apply while the placement that gives it a value did not

-- Which app a tenant belongs to.
--
-- A server hosts many apps over one database (decision 0074 §6), and the tenant
-- is the boundary *inside* an app. Between two apps a tenant id is only a label:
-- both sides name their modules with the same vocabulary and a uuid says nothing
-- about which composition minted it. So every control-plane read — lookup by
-- host, the active-tenant list, the operator routes — has to answer "which app
-- is this operation happening in" before it answers anything else, and the row
-- has to carry the answer. Rule 7 of that decision says a name alone is a
-- convention; this column is the fact the names are checked against.
--
-- A tenant does not move between apps afterwards. The hosts that placed it here
-- are the reason it was placed, and a move would strand every row of the tenant
-- under an app that never issued it — so there is no route that writes this
-- column, and modules/tenant stamps it at the create from the app the
-- composition boot declares.
ALTER TABLE tenants ADD COLUMN app text;

-- The operator's explicit mapping, tenant slug to app slug, for the tenants the
-- hosts cannot place. Read as `slug=app` pairs because a migration takes a value
-- and not a file: kit/app reads the operator's file and declares what it holds
-- through db.Declaration, so the one door a boot has for a fact the database does
-- not hold carries both. Applying it first means an explicit declaration beats an
-- inferred one, which is the order an operator would expect when they wrote both.
UPDATE tenants AS t SET app = m.app
FROM (
	SELECT btrim(split_part(pair, '=', 2)) AS app, btrim(split_part(pair, '=', 1)) AS slug
	FROM unnest(string_to_array(current_setting('platformkit.app_tenants', true), ',')) AS pair
) AS m
WHERE t.app IS NULL AND m.slug <> '' AND m.app <> '' AND t.slug = m.slug;

-- The proof the brief asks for: an existing tenant joins this app only when every
-- host it holds is one the boot declares it serves. A tenant whose hosts are not
-- all here is somebody else's tenant of the same database, and one with no host at
-- all cannot be placed by hosts at all, so it waits for the mapping above. The
-- boot that declares no hosts places nothing — saying "I am the app" without
-- saying where it is served is not evidence about a row somebody else wrote.
UPDATE tenants AS t SET app = current_setting('platformkit.app', true)
WHERE t.app IS NULL
	AND current_setting('platformkit.app', true) IS NOT NULL
	AND coalesce(btrim(current_setting('platformkit.app_hosts', true)), '') <> ''
	AND EXISTS (SELECT 1 FROM tenant_hosts h WHERE h.tenant_id = t.id)
	AND NOT EXISTS (
		SELECT 1 FROM tenant_hosts h
		WHERE h.tenant_id = t.id
			AND NOT (h.host = ANY (
				SELECT btrim(x) FROM unnest(string_to_array(
					current_setting('platformkit.app_hosts', true), ',')) AS x
				WHERE btrim(x) <> ''))
	);

-- The half that refuses. Without it this file would be a write that guesses: the
-- rows left unplaced would carry NULL, and `modules/tenant` would read them under
-- whichever app happened to boot. Naming every tenant it cannot place is the
-- whole point — an operator with a database of tenants and no declaration is
-- exactly the person who must not be allowed to proceed by silence.
--
-- The message names what the boot declared, because the commonest cause is the
-- setting that moved: the slug is nats.app, the hosts are app.hosts, and a
-- deployment that set one and not the other places nothing.
DO $$
DECLARE
	unplaced text;
BEGIN
	IF EXISTS (SELECT 1 FROM tenants WHERE app IS NULL) THEN
		SELECT string_agg(slug, ', ' ORDER BY slug) INTO unplaced FROM tenants WHERE app IS NULL;
		RAISE EXCEPTION
			'platformkit: cannot say which app owns tenants %; the boot declared app=%, hosts=% and tenant mapping %',
			unplaced,
			coalesce(nullif(btrim(current_setting('platformkit.app', true)), ''), '<nothing>'),
			coalesce(nullif(btrim(current_setting('platformkit.app_hosts', true)), ''), '<nothing>'),
			coalesce(nullif(btrim(current_setting('platformkit.app_tenants', true)), ''), '<nothing>');
	END IF;
END
$$;

-- Placed, or the transaction above took the run down with it: either way nothing
-- below is reached with a tenant whose app nobody knows.
ALTER TABLE tenants ALTER COLUMN app SET NOT NULL;

-- What a tenant created from here takes as its app: the one the session declares,
-- and the empty slug when the session declares none, which is the single-app
-- deployment naming itself (kit/config's nats.app unset is that case, and every
-- name kit/appname forms for it is the name it formed before decision 0074).
--
-- Set here rather than on the ADD above, and that is the whole of the proof: a
-- default on the ADD would have stamped every row the database already held with
-- the boot's slug on the strength of nothing at all, which is the write this
-- file's two UPDATEs exist to refuse. A default below the proof only ever reaches
-- a row somebody is creating now.
--
-- What it does not do is make the app for a write that names none: the service
-- stamps the row with the composition's slug (modules/tenant), and this is the
-- floor under a raw INSERT, which the control plane's own routes never are.
ALTER TABLE tenants ALTER COLUMN app SET DEFAULT coalesce(nullif(btrim(current_setting('platformkit.app', true)), ''), '');

-- One operator per app, and not one per database. The index used to be unique on
-- the constant true, which is the database saying "exactly one installation owns
-- this" — true while one composition shared it, and the wrong sentence the moment
-- a second app arrives, because the two operators are two installations that have
-- nothing to do with each other. Grouped by app, the same partial index says the
-- sentence that is true: one operator per app, and every app gets one.
DROP INDEX tenants_operator;
CREATE UNIQUE INDEX tenants_operator ON tenants (app) WHERE operator AND deleted_at IS NULL;

COMMENT ON COLUMN tenants.app IS 'The app whose composition this tenant belongs to: stamped at the create, never rewritten, and what every control-plane read is scoped to (kit/appname, decision 0074 §7).';
