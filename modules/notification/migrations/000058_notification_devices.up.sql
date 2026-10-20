-- The devices a person can be told at.
--
-- Until this file the push channels had a ledger row, a decision and no address:
-- contracts.ChannelPush and ChannelWebPush were carried with Delivery.Target
-- empty, because a device token and a browser subscription belonged to no table.
-- This is that table, and it is deliberately the smallest thing that answers
-- "where do I reach this person" — nothing else is a column because nothing else
-- is read (see modules/notification/contracts/device.go).
--
-- The entities are modules/notification/contracts/device.go; it embeds crud.Base,
-- so the id, the timestamps, the soft delete and the tenant column are the shape
-- every tenant-owned table has. recipient_id is a plain uuid with no foreign key,
-- exactly as notification_preferences.recipient_id (000054): this module never
-- joins the user table.
CREATE TABLE notification_devices (
	id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id    uuid NOT NULL,
	created_at   timestamptz NOT NULL DEFAULT now(),
	updated_at   timestamptz NOT NULL DEFAULT now(),
	deleted_at   timestamptz,

	recipient_id uuid NOT NULL,
	platform     text NOT NULL CHECK (platform IN ('web', 'expo', 'fcm', 'apns')),
	token        text NOT NULL CHECK (length(token) BETWEEN 1 AND 8192)
);

-- The token is the device: the same subscription or push token presented twice
-- is one live row, whoever presented it. Partial on the live rows so that a
-- person who removed their phone and registers it again gets a new row rather
-- than a conflict with the old one — the history of a token is a fact worth
-- keeping, and the register's rows are what says how often a device came back.
CREATE UNIQUE INDEX notification_devices_one_live_token
	ON notification_devices (tenant_id, platform, token) WHERE deleted_at IS NULL;

-- The read every delivery makes: this person's live devices, in the one query the
-- carrier is given. Nothing else is indexed here.
CREATE INDEX notification_devices_recipient
	ON notification_devices (tenant_id, recipient_id) WHERE deleted_at IS NULL;

ALTER TABLE notification_devices ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_devices FORCE ROW LEVEL SECURITY;

CREATE POLICY notification_devices_tenant ON notification_devices
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
