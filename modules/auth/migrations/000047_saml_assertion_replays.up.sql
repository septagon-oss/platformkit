-- The assertions this installation has already spent.
--
-- `modules/auth` verifies a SAML assertion with `github.com/crewjam/saml`, which
-- checks its signature, its audience, its recipient and its window, and cannot
-- check the one thing that is not in the document: whether this same assertion
-- has been presented before. A signed assertion is a bearer credential that
-- anybody who intercepts it, or who is handed a copy by a browser's history or a
-- proxy's log, can post again until `NotOnOrAfter` — and the second presentation
-- opens a session exactly as the first did. This table is what makes "spent" a
-- fact.
--
-- The key is the pair the assertion is identified by, and nothing else: no id
-- column, no user, no session reference. An assertion id is a name the *IdP*
-- chose, so the tenant is part of the identity — two customers fronted by one
-- federation proxy that mints ids the same way are not each other's replays —
-- and a row that named a person would remember who signed in from a credential
-- that is refused before any person is resolved. The claim therefore commits or
-- rolls back with the session it belongs to: a sign-in refused for a closed
-- account leaves the assertion unspent, because burning a credential on somebody
-- else's failure is a denial of service dressed as a security control.
--
-- `expires_at` is the assertion's own `NotOnOrAfter` plus the library's clock
-- skew, and not a retention decision: after that moment the library refuses the
-- assertion whatever this table says, so a row older than it protects nothing and
-- the hourly sweep deletes it (`auth-sweep`, same batched loop as every other
-- credential here). What is stored is an opaque id and two timestamps — no
-- address, no name, no document — so the table holds nothing a data subject could
-- be exported.
CREATE TABLE saml_assertion_replays (
	tenant_id uuid NOT NULL,
	assertion_id text NOT NULL,
	claimed_at timestamptz NOT NULL DEFAULT now(),
	expires_at timestamptz NOT NULL,
	PRIMARY KEY (tenant_id, assertion_id),
	CONSTRAINT saml_assertion_replays_expires_after_claim CHECK (expires_at >= claimed_at)
);

-- The sweep's own arm: it deletes by expiry, and without this index it would
-- full-scan a table whose whole purpose is to be looked up by id.
CREATE INDEX saml_assertion_replays_expiry ON saml_assertion_replays (expires_at);

ALTER TABLE saml_assertion_replays ENABLE ROW LEVEL SECURITY;
ALTER TABLE saml_assertion_replays FORCE ROW LEVEL SECURITY;
CREATE POLICY saml_assertion_replays_tenant ON saml_assertion_replays
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
