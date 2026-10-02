-- A passkey: a second factor whose proof is a signature, not a secret.
--
-- 000031 argued against a challenge table, and the argument was right for TOTP:
-- RFC 6238 already is a challenge — the step is the nonce and the 30-second
-- window is its lifetime. WebAuthn has no such built-in nonce. The challenge is
-- minted by this server, must be remembered until the answer arrives, and must
-- be answerable exactly once, which is the shape of password_tokens and
-- first_factor_proofs: one row, an expiry the sweep walks, and consumption by
-- DELETE, because the row being gone is what "once" means. So this file adds the
-- challenge table 000031 declined, for the one credential kind that needs it.
--
-- Nothing in either table needs auth.factor_key. A passkey's credential material
-- is a public key: it is public by construction, it is worthless to a thief, and
-- sealing it would buy nothing and hide a bug. That is the difference between
-- this table and totp_factors, and it is why a deployment with no factor key can
-- enrol passkeys and answer both sign-in doors (contracts.ErrNoFactorKey covers
-- the enrolment of a shared secret and nothing else).
CREATE TABLE passkey_credentials (
	id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id uuid NOT NULL,
	user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	-- The WebAuthn credential id, as the authenticator handed it back. It is not
	-- a secret and it is not spendable on its own — it identifies the key that
	-- signs, it is not what signs — and it is the only way an assertion finds its
	-- account, so it is read by every sign-in. Bounded so a body a megabyte wide
	-- is refused by the schema as well as by the route.
	credential_id bytea NOT NULL,
	-- The COSE public key. Public, so stored in the open: see the header.
	public_key bytea NOT NULL,
	-- The authenticator's BackupEligible flag, as the registration ceremony
	-- reported it. The standard makes that one flag immutable, the WebAuthn
	-- library enforces the promise against the stored record on every assertion,
	-- and a synced passkey (an iPhone key living in iCloud) reports it true — so
	-- a record without it refuses the largest cohort of real passkeys on the
	-- first thing they try to do. No other stored flag is consulted after
	-- enrolment, so no other stored flag is kept (rule 7).
	backup_eligible boolean NOT NULL DEFAULT false,
	-- The authenticator's counter, as the last successful assertion reported it.
	-- The next assertion must report more, which is the clone rule; both zero is
	-- the library's own exception, because an authenticator that reports no
	-- counter is legal and most of the phones in the room are one.
	sign_count bigint NOT NULL DEFAULT 0,
	-- Set once when a reported count went backwards on a credential that had one.
	-- Read by the statement that verifies the next assertion: once set, this
	-- credential never signs anybody in, and only re-enrolment clears it. Not a
	-- column nothing consults — it is consulted on every use.
	clone_warning boolean NOT NULL DEFAULT false,
	-- What this person called this passkey, so a list of two reads as "phone" and
	-- "laptop" rather than as two dates. Btrimed, bounded, and no control
	-- characters, because it is printed into a page and a terminal.
	name text NOT NULL DEFAULT '',
	created_at timestamptz NOT NULL DEFAULT now(),
	-- One authenticator is one person's factor within a tenant. Tenant-scoped and
	-- deliberately not global: a global unique index would answer a stranger's
	-- insert with a violation naming a row they cannot see, which is a
	-- cross-tenant credential-id oracle. The same authenticator enrolled at two
	-- tenants is two rows and two credentials, because the relying-party id —
	-- which is the host — differs, so they are not the same credential.
	CONSTRAINT passkey_credentials_credential UNIQUE (tenant_id, credential_id),
	CONSTRAINT passkey_credentials_credential_size CHECK (octet_length(credential_id) BETWEEN 1 AND 1024),
	CONSTRAINT passkey_credentials_key_size CHECK (octet_length(public_key) BETWEEN 1 AND 2048),
	CONSTRAINT passkey_credentials_name_shape CHECK (
		name = btrim(name) AND char_length(name) <= 40 AND name !~ '[[:cntrl:]]'
	),
	CONSTRAINT passkey_credentials_count CHECK (sign_count >= 0)
);

-- The assertion lookup is "which account holds this credential id", and the list
-- is "what does this person hold". Both are indexed; neither is a scan.
CREATE INDEX passkey_credentials_user ON passkey_credentials (tenant_id, user_id, created_at DESC);

-- One in-flight ceremony. The row is the server's only copy of what the answer
-- must match, and spending it is deleting it.
CREATE TABLE passkey_challenges (
	id uuid PRIMARY KEY,
	tenant_id uuid NOT NULL,
	-- Who is enrolling. Required exactly when the ceremony is an enrolment: a
	-- sign-in ceremony names nobody, because "Sign in with a passkey" learns the
	-- account from the answer and must not have been told it beforehand.
	user_id uuid REFERENCES users (id) ON DELETE CASCADE,
	-- Which door this ceremony was begun at. Read at finish, which refuses a
	-- challenge minted for another door: the door is the row's fact and never the
	-- caller's, which is what stops a second-factor ceremony from being presented
	-- at the usernameless door to skip the password.
	kind text NOT NULL,
	-- webauthn.SessionData as JSON: the challenge, the relying-party id this
	-- ceremony was begun under, the origin, the allowed credential ids, the
	-- expiry. It holds no secret beyond the challenge nonce, which the browser
	-- holding the ceremony id already has, and no material of a credential.
	session jsonb NOT NULL,
	expires_at timestamptz NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	CONSTRAINT passkey_challenges_kind CHECK (kind IN ('register', 'second-factor', 'sign-in')),
	CONSTRAINT passkey_challenges_register_names_a_user CHECK (
		(user_id IS NOT NULL) = (kind = 'register')
	),
	CONSTRAINT passkey_challenges_session_is_an_object CHECK (jsonb_typeof(session) = 'object'),
	CONSTRAINT passkey_challenges_expires_after_created CHECK (expires_at >= created_at)
);

CREATE INDEX passkey_challenges_expires_at ON passkey_challenges (expires_at);

ALTER TABLE passkey_credentials ENABLE ROW LEVEL SECURITY;
ALTER TABLE passkey_credentials FORCE ROW LEVEL SECURITY;
CREATE POLICY passkey_credentials_tenant ON passkey_credentials
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));

ALTER TABLE passkey_challenges ENABLE ROW LEVEL SECURITY;
ALTER TABLE passkey_challenges FORCE ROW LEVEL SECURITY;
CREATE POLICY passkey_challenges_tenant ON passkey_challenges
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));

-- Whether a passkey may be the whole sign-in at a tenant.
--
-- A passkey is a second factor the moment passkey_credentials has a row for a
-- person: nothing here is needed for that, and nothing here turns it on. What
-- this gates is the wider use — "Sign in with a passkey", with no password
-- offered first — which a tenant may not want even while its people use passkeys
-- as their second factor: usernameless sign-in is a different promise about what
-- a stolen password costs, and the person who decides it is the tenant's
-- administrator, not this schema.
--
-- The row lives here, in the auth module's own migration source, rather than as
-- a column on the kernel's `tenants`, for one reason that is a test rather than a
-- taste: apps/platformkit/app_test.go's legacyLayout takes "the release from
-- before modules owned their own SQL" to be every file up to the highest the
-- kernel itself ships, so a kernel file numbered above 000030 drags the module
-- files that postdate the split (auth's 000031, 000032, 000033) into that
-- installation's ledger — a ledger no release ever left behind. A fact that gates
-- only this module's own door is this module's row to keep, and it is read inside
-- the tenant transaction that the request already resolved, the same as every
-- other table in this file.
--
-- No row means false, and no row is what every installation that exists has: the
-- gate is `sign_in` when a row is there and off when it is not, so turning the
-- door on is a write rather than a migration, and a tenant that never wants it
-- carries nothing. DEFAULT false is the whole of the rollout policy; the two
-- usernameless routes answer ErrPasskeySignInOff until somebody writes this row.
CREATE TABLE passkey_settings (
	-- One row per tenant, and the primary key is the only predicate the read
	-- needs: the row is the tenant's own setting and nothing else.
	tenant_id uuid PRIMARY KEY,
	-- The one column. `name`, `created_at` and `updated_at` would be columns
	-- nothing consults, so this table has none of them (rule 7): the audit trail
	-- of who turned the door on is the record the audit module keeps, not a
	-- timestamp on a settings row no command reads.
	sign_in boolean NOT NULL DEFAULT false
);

ALTER TABLE passkey_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE passkey_settings FORCE ROW LEVEL SECURITY;
CREATE POLICY passkey_settings_tenant ON passkey_settings
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
