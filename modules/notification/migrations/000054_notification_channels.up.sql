-- Which channel each row of the ledger is, and what a person chose about being
-- told.
--
-- Two things the ledger could not say before this file. The channel list could
-- not tell a browser subscription from a phone, and delivery_ledger_coverage
-- counts terminal rows per (notification_id, channel), so one `push` row for two
-- transports would count a phone for a browser and the ratio would flatter the
-- deployment. And the only answer to "may we mail this person" was whether the
-- row had an address: nobody had chosen anything, so the table that holds a
-- choice did not exist.
--
-- The entities are modules/notification/contracts/preferences.go; each embeds
-- crud.Base, so the id, the timestamps, the soft delete and the tenant column
-- are the shape every tenant-owned table has.

ALTER TABLE notification_deliveries DROP CONSTRAINT notification_deliveries_channel_check;
ALTER TABLE notification_deliveries ADD CONSTRAINT notification_deliveries_channel_check
	CHECK (channel IN ('in_app', 'email', 'push', 'web_push', 'webhook'));

CREATE TABLE notification_preferences (
	id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id     uuid NOT NULL,
	created_at    timestamptz NOT NULL DEFAULT now(),
	updated_at    timestamptz NOT NULL DEFAULT now(),
	deleted_at    timestamptz,

	recipient_id  uuid NOT NULL,
	intent        text NOT NULL DEFAULT '',
	channel       text NOT NULL CHECK (channel IN ('email', 'push', 'web_push', 'webhook')),
	enabled       boolean NOT NULL
);

-- One row per (person, intent, channel): SetChannel updates the row it finds
-- rather than appending a second answer nobody can read.
CREATE UNIQUE INDEX notification_preferences_one_answer
	ON notification_preferences (tenant_id, recipient_id, intent, channel)
	WHERE deleted_at IS NULL;

-- The read the notice path makes: this person's blanket rows and the one for
-- this intent, in the one query Decide is fed. Nothing else is indexed here —
-- a settings page is a handful of rows read by primary key.
CREATE INDEX notification_preferences_recipient
	ON notification_preferences (tenant_id, recipient_id) WHERE deleted_at IS NULL;

-- One person's quiet window. Minutes from midnight in a named zone, because
-- quiet hours are the same hours in March and in September and an UTC offset is
-- not: the column is an IANA name so the window does not move when the far end
-- changes its clocks.
CREATE TABLE notification_quiet_hours (
	id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id     uuid NOT NULL,
	created_at    timestamptz NOT NULL DEFAULT now(),
	updated_at    timestamptz NOT NULL DEFAULT now(),
	deleted_at    timestamptz,

	recipient_id  uuid NOT NULL,
	start_minute  integer NOT NULL CHECK (start_minute >= 0 AND start_minute < 1440),
	end_minute    integer NOT NULL CHECK (end_minute >= 0 AND end_minute < 1440),
	time_zone     text NOT NULL
);

CREATE UNIQUE INDEX notification_quiet_hours_one_window
	ON notification_quiet_hours (tenant_id, recipient_id) WHERE deleted_at IS NULL;

ALTER TABLE notification_preferences ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_preferences FORCE ROW LEVEL SECURITY;

CREATE POLICY notification_preferences_tenant ON notification_preferences
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));

ALTER TABLE notification_quiet_hours ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_quiet_hours FORCE ROW LEVEL SECURITY;

CREATE POLICY notification_quiet_hours_tenant ON notification_quiet_hours
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
