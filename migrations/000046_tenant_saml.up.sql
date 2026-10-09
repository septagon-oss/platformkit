-- Which SAML identity provider each tenant's people sign in against, and what
-- an address that provider vouches for means here.
--
-- This is 000030 repeated for the other half of the enterprise identity market.
-- A tenant whose directory speaks only SAML 2.0 — still the only protocol many
-- federation servers offer — had, until this file, no way to be served by an
-- installation whose single sign-on was one OIDC issuer: not the issuer's, not
-- the client's, and no assertion anyone could verify. The decision is 0028's
-- again and shaped the same way: the facts live on this row, the mechanism stays
-- in `modules/auth`, and the resolution happens per request inside the
-- transaction the `Host` already chose, so two tenants on one process are
-- verified by two IdPs against one ACS URL each.
--
-- There is no secret here and none can be. An IdP's metadata — its entity ID,
-- its SSO location, its signing certificate — is public by construction: it is
-- what an IdP publishes at its own metadata URL, and what a certificate
-- verifies is a signature, not a secret. So `modules/auth` needs no
-- `contracts.Secrets` lookup on this leg, an absent certificate is a refusal at
-- the write rather than a 503 at sign-in, and the outbox payload of
-- `tenant.saml_set` carries the entity ID it replaced and never the document
-- itself (which is multi-kilobyte, and a payload is copied into `audit_events`).
--
-- The service provider's own signing key is absent for the opposite reason: an
-- SP key *is* a secret, its storage, rotation and vault are a decision this
-- repository has not made, and a column that names a key nobody can rotate is a
-- column somebody will fill with a pasted PEM. `AuthnRequest`s are therefore
-- unsigned and the SP metadata carries no KeyDescriptor; the assertion signature
-- this table makes verifiable is the IdP's, and it is the one the brief names.
ALTER TABLE tenants ADD COLUMN saml_entity_id text;
ALTER TABLE tenants ADD COLUMN saml_metadata_url text;
ALTER TABLE tenants ADD COLUMN saml_metadata_xml text;
ALTER TABLE tenants ADD COLUMN saml_email_attribute text;
ALTER TABLE tenants ADD COLUMN saml_registration text NOT NULL DEFAULT 'existing';
ALTER TABLE tenants ADD COLUMN saml_roles text[] NOT NULL DEFAULT '{}';

-- The three modes are 000030's three, with the same default and the same
-- meaning: `disabled` is a tenant saying SAML is not what we use, `existing` is
-- every tenant that exists and refuses an address this tenant has no account
-- for, `provision` makes that person with the named roles. One CHECK per column
-- family keeps a SAML row from being readable only if the OIDC row beside it is
-- absent, which would make switching a customer from one protocol to the other a
-- write that has to clear both at once.
ALTER TABLE tenants ADD CONSTRAINT tenants_saml_registration_known CHECK (
	saml_registration IN ('disabled', 'existing', 'provision')
);

-- Half a provider is unstatable. An entity ID with no metadata is an audience
-- nobody can be verified against, metadata with no entity ID is a document with
-- no name to check, and an email attribute unnamed is an assertion whose subject
-- would be guessed at — all three are configurations no test could exercise,
-- which is the shape 000030 refuses for the same reason. The entity ID rather
-- than the XML marks presence: a tenant that stored a document and no entity ID
-- has, by this CHECK, stored nothing storable.
ALTER TABLE tenants ADD CONSTRAINT tenants_saml_complete CHECK (
	(saml_entity_id IS NULL AND saml_metadata_url IS NULL AND saml_metadata_xml IS NULL
		AND saml_email_attribute IS NULL
		AND saml_registration = 'existing' AND saml_roles = '{}')
	OR
	(saml_entity_id IS NOT NULL AND saml_email_attribute IS NOT NULL
		AND (saml_metadata_url IS NOT NULL OR saml_metadata_xml IS NOT NULL)
		AND (saml_registration <> 'provision' OR cardinality(saml_roles) > 0))
);

-- Which provider a company signs in against is readable by that company and
-- chosen by nobody inside it: the columns ride the policy 000006 puts over
-- `tenants` — a tenant transaction reads its own row and writes nothing anywhere
-- on this table — and no new policy is created here, for the reason 000030 gives
-- at its own close. `modules/tenant`'s `SAMLSettingsOf` is that read; `SetSAML`
-- and `ClearSAML` take a `db.Tx[db.System]`.
--
-- data: exempt reason: this revision adds four nullable columns, one column with
-- a default every existing row already satisfies, one column with a default and a
-- CHECK both of which every existing row already satisfies, and two CHECKs no
-- existing row can violate; it writes no rows, so no `phase=data` half stands
-- beside it. migrations/README.md says what this line means and where a file that
-- does write rows has to take them.

COMMENT ON COLUMN tenants.saml_metadata_xml IS
	'platformkit:tenant-scoping-exempt: control-plane column; the IdP''s own published document, which is public by construction and is kept so a URL that goes offline at the IdP does not close this tenant''s door';
COMMENT ON COLUMN tenants.saml_entity_id IS
	'platformkit:tenant-scoping-exempt: control-plane column; this installation''s SAML identity for that tenant, which is the audience every assertion must name';
