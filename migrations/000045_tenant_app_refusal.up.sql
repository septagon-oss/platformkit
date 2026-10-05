-- The two halves of a placement, in one file: the walk that places, and the
-- sentence it says when it cannot place.
--
-- migrations/000043_tenant_app states the refusal inside itself, as a DO block: the
-- file is one transaction, so the rows it just failed to place are the rows it can
-- name. A placement that runs in windows cannot do that — the half-drained run
-- would refuse over rows its own earlier windows had already placed, and the
-- message would name tenants the operator had nothing to do with. So the refusal
-- is a function, and the placement calls it with the list it computed rather than
-- with a re-read of the table: a data-modifying statement sees the snapshot it
-- started with, so the rows this statement placed are still empty from inside it,
-- and a helper that went looking would name them as unplaced.
--
-- It reads the same three declarations as 000043 and prints the same sentence for
-- the same reason: the commonest cause is the setting that moved, and an operator
-- with a database of tenants and no declaration is exactly the person who must not
-- be allowed to proceed by silence. What it does not do is decide anything: it is
-- called with the list of tenants nobody named, and it refuses unless it was called
-- with none.
--
-- The placement itself, and why it is a function beside the sentence.
--
-- The walk that decides which app a tenant belongs to — the explicit pair first,
-- then the host proof — has to live in exactly one place, because it is asked by
-- two callers that can never be allowed to disagree about one row: the release
-- file that places the rows on the way past (`000046`, one window per statement),
-- and every later boot that carries a declaration the release did not have. The
-- second caller exists because a migration file is spent the moment it drains:
-- the supported upgrade that installs the release while the deployment still
-- names no app drains 000046 over rows it must not touch, history marks the file
-- done, and the operator's later `nats.app` would place nothing at all if the
-- placement lived only in that file — the tenants would sit outside both named
-- apps forever, served on the legacy durable by a composition that has stopped
-- naming it. So the file is the *first* run of this function and no longer the
-- only one, and `kit/app.Migrate` and `kit/app.Drain` ask it of every boot that
-- declares an app or a mapping.
--
-- What it returns, and what it refuses not to do: the slugs still empty *within
-- the keys it was handed*, in slug order, or every remaining row when it was
-- handed none. It never refuses — the guard over who may be refused for a row
-- nobody placed is the caller's (`000046`'s outer query), and this function is
-- the same call from a boot that merely reports what is left. One statement per
-- half, and the halves are two statements on purpose: a data-modifying statement
-- reads the snapshot it began with, so the list of "still nobody's" has to be
-- taken *after* the write, in its own statement, or a placement would name the
-- rows it had just placed as the ones it could not place.
CREATE FUNCTION platformkit_place_tenants(own_app text, own_hosts text, own_map text, batch_keys uuid[])
RETURNS text LANGUAGE plpgsql AS $$
DECLARE
	unplaced text;
BEGIN
	WITH declared AS (
		-- The operator's explicit placement, `slug=app` pairs. Same grammar and same
		-- precedence as 000043: an explicit pair beats a host inference. A pair with
		-- either half missing names nothing and is dropped by the two `<> ''` tests
		-- rather than placing a tenant under an empty app.
		SELECT btrim(split_part(pair, '=', 1)) AS slug, btrim(split_part(pair, '=', 2)) AS app
		FROM unnest(string_to_array(own_map, ',')) AS pair
	),
	placeable AS (
		SELECT t.id, coalesce(
			(SELECT d.app FROM declared d WHERE d.slug = t.slug AND d.slug <> '' AND d.app <> ''),
			nullif(btrim(coalesce(own_app, '')), '')) AS app
		FROM tenants AS t
		WHERE t.app = ''
			AND (batch_keys IS NULL OR t.id = ANY (batch_keys))
			AND (
				EXISTS (SELECT 1 FROM declared d WHERE d.slug = t.slug AND d.slug <> '' AND d.app <> '')
				OR (
					-- The proof, and it is the whole of it: this boot named itself and
					-- named the hosts it serves, and every host this tenant holds is one
					-- of them. A tenant with no host is not placed by hosts at all, and a
					-- boot that names no hosts places nothing.
					nullif(btrim(coalesce(own_app, '')), '') IS NOT NULL
					AND nullif(btrim(coalesce(own_hosts, '')), '') IS NOT NULL
					AND EXISTS (SELECT 1 FROM tenant_hosts h WHERE h.tenant_id = t.id)
					AND NOT EXISTS (
						SELECT 1 FROM tenant_hosts h
						WHERE h.tenant_id = t.id
							AND NOT (h.host = ANY (
								SELECT btrim(x) FROM unnest(string_to_array(own_hosts, ',')) AS x
								WHERE btrim(x) <> '')))
				)
			)
	)
	UPDATE tenants AS t
	SET app = p.app
	FROM placeable p
	WHERE t.id = p.id;

	-- The second statement, and the only reason it is a second statement: this one
	-- sees the rows the one above wrote, so what it lists is what is left rather than
	-- what this run touched.
	SELECT coalesce(string_agg(slug, ', ' ORDER BY slug), '') INTO unplaced
	FROM tenants
	WHERE app = '' AND (batch_keys IS NULL OR id = ANY (batch_keys));
	RETURN unplaced;
END
$$;

COMMENT ON FUNCTION platformkit_place_tenants(text, text, text, uuid[]) IS
	'platformkit: places every tenant still naming no app under the app the passed declaration proves for it (explicit slug=app pair first, then the host proof), within the key list it is handed or over the whole table when handed none, and returns the slugs left empty; called by the placement window of migrations/000046 and by every boot of kit/app that names an app or a mapping, so the two cannot disagree about a row';

-- Which caller may hand it a list is not its decision either, and that matters: a
-- boot that named no app asserts nothing about whose anybody's tenant is, and the
-- placement that stopped it would be refusing this release to every single-app
-- deployment (see the guard on the statement at the bottom of
-- migrations/000046_tenant_app_place.up.sql). So the caller asks the question and
-- this prints the answer, which is the only division of the two jobs that keeps the
-- sentence in one place while leaving "who is answerable for a row" with the
-- statement that writes the row.
CREATE FUNCTION platformkit_refuse_unplaced_tenants(unplaced text) RETURNS text
LANGUAGE plpgsql AS $$
BEGIN
	IF unplaced IS NULL OR unplaced = '' THEN
		RETURN '';
	END IF;
	RAISE EXCEPTION
		'platformkit: cannot say which app owns tenants %; the boot declared app=%, hosts=% and tenant mapping %',
		unplaced,
		coalesce(nullif(btrim(current_setting('platformkit.app', true)), ''), '<nothing>'),
		coalesce(nullif(btrim(current_setting('platformkit.app_hosts', true)), ''), '<nothing>'),
		coalesce(nullif(btrim(current_setting('platformkit.app_tenants', true)), ''), '<nothing>');
END
$$;

COMMENT ON FUNCTION platformkit_refuse_unplaced_tenants(text) IS
	'platformkit: refuses a placement that left a tenant with no app, naming every such tenant and what the boot declared; called by the data placement with the list it computed, only of a boot that named itself, and by nothing that can answer for a tenant itself';
