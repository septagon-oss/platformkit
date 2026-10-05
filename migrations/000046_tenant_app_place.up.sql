-- pkit: phase=data
-- pkit: batch=5000
-- pkit: table=tenants
-- The placement 000043 could not make, forward, for the tenants an app-less boot
-- stamped with the empty slug.
--
-- 000043 proves its rows against the hosts the boot declares and refuses the rest.
-- It runs before `app` becomes NOT NULL, so a database that was already app-less at
-- that point has every row ''  — the column's own default answers '' for a session
-- that named no app — and the file that placed the NULLs is finished, checksummed
-- and immutable. What an operator holding such a database has in front of them is
-- the same proof over again with a different predicate, which is what this file is:
-- the explicit mapping wins, the host proof places the tenants every one of whose
-- hosts this composition serves, and anything left over is named and refused by the
-- function 000045 puts there.
--
-- Three things it deliberately is not. It is not a re-placement: `app = ''` is the
-- only row it can touch, because a tenant does not move between apps once it has one
-- and this file has no evidence about a row somebody already answered. It is not a
-- guess about a tenant with no host and no mapping — that tenant stops the drain.
-- And it is not one statement per tenant: the choice of app is computed once, in
-- `placeable`, so the write and the refusal cannot drift apart about who is
-- placeable, which is the only way this file could tell two stories about one row.
--
-- It drains where every data file drains — the boot that declares the mapping, or
-- the worker's tick that carries the same declaration
-- (kit/app/migrations.go, db.BackfillDeclaring) — and the run that reaches the
-- refusal keeps its cursor, so the retry after the operator fixes the setting is the
-- same walk and takes the rows it never reached.
WITH declared AS (
	-- The operator's explicit placement, `slug=app` pairs, from the session the boot
	-- declared. Same grammar and same precedence as 000043: an explicit pair beats a
	-- host inference. A pair with either half missing names nothing and is dropped by
	-- the two `<> ''` tests below rather than placing a tenant under an empty app.
	SELECT btrim(split_part(pair, '=', 1)) AS slug, btrim(split_part(pair, '=', 2)) AS app
	FROM unnest(string_to_array(current_setting('platformkit.app_tenants', true), ',')) AS pair
),
placeable AS (
	SELECT t.id, coalesce(
		(SELECT d.app FROM declared d WHERE d.slug = t.slug AND d.slug <> '' AND d.app <> ''),
		current_setting('platformkit.app', true)) AS app
	FROM tenants AS t
	WHERE t.app = ''
		AND (
			EXISTS (SELECT 1 FROM declared d WHERE d.slug = t.slug AND d.slug <> '' AND d.app <> '')
			OR (
				-- The proof, and it is the whole of it: this boot named itself and
				-- named the hosts it serves, and every host this tenant holds is one
				-- of them. A tenant with no host is not placed by hosts at all, and a
				-- boot that names no hosts places nothing.
				coalesce(nullif(btrim(current_setting('platformkit.app', true)), ''), '') <> ''
				AND coalesce(nullif(btrim(current_setting('platformkit.app_hosts', true)), ''), '') <> ''
				AND EXISTS (SELECT 1 FROM tenant_hosts h WHERE h.tenant_id = t.id)
				AND NOT EXISTS (
					SELECT 1 FROM tenant_hosts h
					WHERE h.tenant_id = t.id
						AND NOT (h.host = ANY (
							SELECT btrim(x) FROM unnest(string_to_array(
								current_setting('platformkit.app_hosts', true), ',')) AS x
							WHERE btrim(x) <> '')))
			)
		)
),
moved AS (
	UPDATE tenants AS t
	SET app = p.app
	FROM placeable p
	WHERE t.id = p.id
		AND t.id IN (SELECT id FROM batch)
	RETURNING 1
)
SELECT platformkit_refuse_unplaced_tenants((
	SELECT string_agg(slug, ', ' ORDER BY slug)
	FROM tenants AS t
	WHERE t.app = ''
		AND t.id NOT IN (SELECT id FROM placeable)));
