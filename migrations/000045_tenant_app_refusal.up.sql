-- The sentence a placement says when it cannot place, in one place.
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
