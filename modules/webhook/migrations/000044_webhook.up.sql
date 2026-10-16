-- The endpoints a tenant registers so that a machine of its own hears what
-- happened here, and the one signing secret each of them holds.
--
-- Owner: webhook. Two tables, because the credential and the resource it signs
-- for are read by different halves of the system: everything about an endpoint
-- is listable, filterable and screenable, and nothing may read the secret back.
-- A secret column on webhook_endpoints would be a secret on the generated form
-- and in the payload of webhook.endpoint.created, which the audit trail keeps
-- for a year. So the cleartext exists only inside the process that signs, and
-- only for the length of one attempt.
--
-- The secret is encrypted rather than hashed because HMAC needs the plaintext
-- back on every send: modules/auth's hash-only rule is for credentials the
-- platform checks, and this one is a key the platform uses. The key that wraps
-- it is deployment configuration the composition reads and a tenant can neither
-- read nor set.
--
-- No foreign key to the endpoint: this module's two tables are joined by a Go
-- value written by one command, the way every other module's cross-table joins
-- are, and a soft-deleted endpoint keeps its secret row for the same reason it
-- keeps its deliveries — a delete must not take the trail away.
CREATE TABLE webhook_endpoints (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        uuid NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at       timestamptz NOT NULL DEFAULT clock_timestamp(),
    deleted_at       timestamptz,
    name             varchar(120) NOT NULL,
    url              varchar(2048) NOT NULL,
    -- The event types this endpoint wants. text[] rather than jsonb because the
    -- only question ever asked of it is "does this list contain that name", and
    -- `events @> ARRAY[name]::text[]` answers it with a GIN index.
    events           text[] NOT NULL DEFAULT '{}',
    status           varchar(16) NOT NULL DEFAULT 'enabled' CHECK (status IN ('enabled', 'disabled')),
    disabled_at      timestamptz,
    disabled_reason  text NOT NULL DEFAULT '',
    -- The 3-day fault clock: the first failure after the last success. COALESCE
    -- on write is what makes it the *first*, so a run of failures has one date.
    failing_since    timestamptz,
    last_success_at  timestamptz,
    -- Who registered it, which is who a disablement notice addresses.
    created_by       uuid NOT NULL,
    CHECK (status <> 'disabled' OR disabled_at IS NOT NULL)
);

-- The fan-out's own scan: the enabled endpoints of one tenant, which is every
-- read that reaches this table on the delivery path.
CREATE INDEX webhook_endpoints_tenant_enabled ON webhook_endpoints (tenant_id)
	WHERE status = 'enabled' AND deleted_at IS NULL;
-- The name match beside it.
CREATE INDEX webhook_endpoints_events ON webhook_endpoints USING gin (events);

ALTER TABLE webhook_endpoints ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_endpoints FORCE ROW LEVEL SECURITY;

CREATE POLICY webhook_endpoints_tenant ON webhook_endpoints
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));

-- The credential, one row per endpoint, written by the create and the rotate and
-- read by nothing but the sender. previous_* is the grace window: a message
-- already queued was addressed to a receiver holding the old secret, and after
-- the window the next rotation overwrites it.
CREATE TABLE webhook_endpoint_secrets (
    endpoint_id          uuid PRIMARY KEY,
    tenant_id            uuid NOT NULL,
    current_nonce        bytea NOT NULL,
    current_ciphertext   bytea NOT NULL,
    previous_nonce       bytea,
    previous_ciphertext  bytea,
    previous_expires_at  timestamptz,
    rotated_at           timestamptz NOT NULL,
    created_at           timestamptz NOT NULL DEFAULT clock_timestamp(),
    -- Half a pair or none of it: a ciphertext with no nonce could not be opened.
    CHECK (previous_nonce IS NULL = (previous_ciphertext IS NULL))
);

ALTER TABLE webhook_endpoint_secrets ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_endpoint_secrets FORCE ROW LEVEL SECURITY;

CREATE POLICY webhook_endpoint_secrets_tenant ON webhook_endpoint_secrets
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
