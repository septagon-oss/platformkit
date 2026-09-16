-- Notifications: one row per person told one thing.
--
-- The entity is modules/notification/contracts/notification.go, and it embeds
-- crud.Base, so the id, the timestamps, the soft delete and the tenant column
-- are the shape every tenant-owned table has.
--
-- recipient_id is a user id and there is no foreign key to users, which is what
-- "cross-module dependencies are Go interfaces" costs at the database: the
-- notification module never names the user module, and the address a message is
-- sent to is resolved through an interface the application satisfies.
CREATE TABLE notifications (
	id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id    uuid NOT NULL,
	created_at   timestamptz NOT NULL DEFAULT now(),
	updated_at   timestamptz NOT NULL DEFAULT now(),
	deleted_at   timestamptz,

	recipient_id uuid NOT NULL,
	title        text NOT NULL,
	body         text NOT NULL DEFAULT '',
	-- Where the notice points, as a path within the application. It is not a
	-- URL: a notice that could carry an absolute one is a notice somebody can
	-- use to send a tenant's users somewhere else.
	link         text NOT NULL DEFAULT '',
	-- NULL until the recipient has seen it. A timestamp rather than a flag,
	-- because "when" is the question a support conversation actually asks.
	read_at      timestamptz
);

-- The only list there is: one person's notices, newest first. There is no
-- tenant-wide list, which is why this module mounts no rest.Spec — a Spec's
-- list route is the whole tenant, and these rows are addressed to somebody.
CREATE INDEX notifications_tenant_recipient ON notifications (tenant_id, recipient_id, created_at DESC, id)
	WHERE deleted_at IS NULL;

ALTER TABLE notifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE notifications FORCE ROW LEVEL SECURITY;

CREATE POLICY notifications_tenant ON notifications
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
