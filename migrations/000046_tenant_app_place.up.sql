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
-- function 000045 puts there — but refused of a boot that named itself, and of no
-- other. The guard and the reason for it are on the statement at the bottom.
--
-- Three things it deliberately is not. It is not a re-placement: `app = ''` is the
-- only row it can touch, because a tenant does not move between apps once it has one
-- and this file has no evidence about a row somebody already answered. It is not a
-- guess about a tenant with no host and no mapping — that tenant stops the drain of
-- a boot that named itself, and stays where it is for every other.
-- And it is not one statement per tenant: the choice of app is computed once, in
-- `platformkit_place_tenants` (000045), so the write and the refusal cannot drift
-- apart about who is placeable, which is the only way this file could tell two
-- stories about one row. That function is what makes this file the *first* run of
-- a placement rather than the only one: the file is spent when it drains, and the
-- boot that names an app after an upgrade that named none asks the same function
-- about the same rows — see kit/app/placement.go.
--
-- It refuses only the boot that named itself. That guard is the line `make check`'s
-- rehearsal drew (v1.1.0 -> this tree, measured 2026-10-05): the copy that step
-- migrates holds the one tenant the previous release bootstrapped, and the boot it is
-- handed names hosts and no `nats.app` — the deployment of one app that has not taken
-- a slug, which 000043's own column default says is a shape this kernel supports. A
-- boot like that asserts nothing about whose anybody's tenant is, so refusing it
-- refuses the release to every deployment that has not joined the two-app vocabulary:
-- the write that takes the last one away, held over a boot that came to name none.
-- What it leaves is exactly what it found — still '', still on the legacy durable,
-- still served, because kit/events' move renames nothing for an app-less tenant — and
-- the boot that later names itself, or is handed a mapping naming them, places them.
-- The boot that does name itself is refused as before: it has said "I am collect, and I
-- serve these hosts", and a tenant left at '' under that statement is one its own
-- declaration cannot answer for — the operator 000043's sentence was written for.
--
-- It drains where every data file drains — the boot that declares the mapping, or
-- the worker's tick that carries the same declaration
-- (kit/app/migrations.go, db.BackfillDeclaring) — and the run that reaches the
-- refusal keeps its cursor, so the retry after the operator fixes the setting is the
-- same walk and takes the rows it never reached.
SELECT CASE
	-- '' when the boot named no app: the guard the comment above argues for, kept in
	-- the caller rather than in the function, because "who may be refused for a row
	-- nobody placed" is this statement's decision and the function's only job is to
	-- print the sentence for whoever has to make it.
	WHEN coalesce(nullif(btrim(current_setting('platformkit.app', true)), ''), '') <> '' THEN
		platformkit_refuse_unplaced_tenants(platformkit_place_tenants(
			current_setting('platformkit.app', true),
			current_setting('platformkit.app_hosts', true),
			current_setting('platformkit.app_tenants', true),
			(SELECT array_agg(id) FROM batch)))
	ELSE
		-- The same walk, with its answer discarded: a boot that named no app places
		-- what its mapping names and prints nothing about the rest. The empty array on
		-- the NULL side is the window itself — `(SELECT array_agg(id) FROM batch)` over
		-- no rows is NULL, and NULL is this function's "the whole table", which a drain
		-- part-way through a window must never mean.
		platformkit_place_tenants(
			current_setting('platformkit.app', true),
			current_setting('platformkit.app_hosts', true),
			current_setting('platformkit.app_tenants', true),
			coalesce((SELECT array_agg(id) FROM batch), ARRAY[]::uuid[]))
END;
