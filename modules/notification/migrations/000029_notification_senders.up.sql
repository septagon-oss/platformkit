-- The address this tenant presents itself as, and what has to be true before
-- anything leaves the installation from it.
--
-- Until now every mail this module sent was from the address in the
-- deployment's own configuration, which is right for the envelope — MAIL FROM is
-- one per binary — and wrong for the header, which is a tenant's name and
-- belongs to the tenant. contracts/sender.go is the entity; this is its table.
-- The two addresses answer different questions, and only one of them is a
-- column here.
--
-- What is deliberately not a column: the DKIM private key. A table is copied by
-- every backup, read by every analyst with a replica and shipped to every
-- restore test; signing key material belongs in the deployment's own secret
-- store, which is where the composition reads it and hands it to the mail
-- provider. A verified sender whose key the deployment does not hold suppresses
-- mail with a reason that names the deployment, which is why contracts.Sender
-- can carry Key and gorm:"-" at the same time.
--
-- status is 'pending' until somebody with a resolver, or an operator who has
-- looked, records the proof: a caller may not post it, and an address somebody
-- typed is not a domain that consents to being signed for.
CREATE TABLE notification_senders (
	id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id      uuid NOT NULL,
	created_at     timestamptz NOT NULL DEFAULT now(),
	updated_at     timestamptz NOT NULL DEFAULT now(),
	deleted_at     timestamptz,

	domain         text NOT NULL,
	selector       text NOT NULL,
	from_name      text NOT NULL,
	from_address   text NOT NULL,
	reply_to       text NOT NULL DEFAULT '',
	status         text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'verified')),
	-- The value this row asks the tenant to publish at
	-- _platformkit-verify.<domain>. Minted with the row, reminted when the
	-- domain changes: it is what makes verification something the customer's
	-- own administrator can do.
	token          text NOT NULL DEFAULT '',
	-- What the check saw, in one line, so a dispute two years later has an
	-- answer that is not a guess about who was allowed to click the button.
	proof          text NOT NULL DEFAULT '',
	verified_at    timestamptz
);

-- One live sender per tenant. A tenant may change its address — Put replaces
-- the row — and the partial index is what lets a withdrawn sender's id be
-- remembered while its replacement takes the slot.
CREATE UNIQUE INDEX notification_senders_one_live
	ON notification_senders (tenant_id) WHERE deleted_at IS NULL;

ALTER TABLE notification_senders ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_senders FORCE ROW LEVEL SECURITY;

CREATE POLICY notification_senders_tenant ON notification_senders
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
