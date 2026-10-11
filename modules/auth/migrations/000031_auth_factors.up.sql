-- A second factor: what a person proves beside the password.
--
-- A password proves something was typed. A second factor proves something was
-- carried. Until this file the first proof was the only proof, so a leaked
-- password — a reused one, a mailbag, a keylogger — was the account, and
-- T-0013's item 6 stayed red while every session and every revocation in this
-- schema was guarded by it.
--
-- Two tables, and no third.
--
-- The obvious third is factor_challenges: a row per sign-in that is waiting for
-- a code. This module does not write one, because RFC 6238 already is a
-- challenge — the step is the nonce, the 30-second window is its lifetime, and
-- the only state a challenge table would add is "which steps has this person
-- already spent", which is one integer on the factor itself. A challenge row
-- would be written, read once and deleted by the sweep, which is rule 7
-- ("a field nothing reads is not written") with an extra round trip. So
-- last_step holds it, and last_step is read by the one statement that decides
-- whether a code is spendable.
CREATE TABLE totp_factors (
	id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id uuid NOT NULL,
	user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	-- The shared secret, sealed. It is not hashed, because verification needs
	-- the value and not a comparison of derivations, and it is not plaintext,
	-- because a factor secret in a backup is a factor an attacker can copy and
	-- use forever (sessions.id_hash says what a dumped credential costs). What
	-- is stored is nacl/secretbox over the deployment's factor key; without
	-- that key (config auth.factor_key) the enrolment routes answer 503 and no
	-- row is ever written, so this column can never hold a plaintext secret.
	secret bytea NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	-- The highest RFC 6238 step ever accepted for this factor. A code is
	-- spendable while its step is greater than this: one UPDATE guarded by
	-- last_step < ? is the replay refusal and the replay record, and it is
	-- atomic in the way a "did we see this code" lookup is not.
	last_step bigint NOT NULL DEFAULT 0
);

-- A person may hold more than one factor — two phones is a person, not a
-- misconfiguration — and withdrawing one must not lock them out, which is the
-- reason WithdrawFactor refuses the last rather than allowing any.
CREATE INDEX totp_factors_user ON totp_factors (tenant_id, user_id);

CREATE TABLE recovery_codes (
	id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id uuid NOT NULL,
	user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	-- SHA-256, not argon2id, and this is the same call sessions.id_hash makes:
	-- the input is 128 bits from crypto/rand, so there is no dictionary to run
	-- and nothing for a slow hash to buy. It is also argon2id's cost profile
	-- per sign-in on a page a person may be locked out of.
	code_hash bytea NOT NULL UNIQUE,
	created_at timestamptz NOT NULL DEFAULT now(),
	-- Spent, not deleted: the sweep takes the row, and until then the trail can
	-- say which code was used and when. Nothing reads used_at except the
	-- statement that sets it.
	used_at timestamptz,
	CONSTRAINT recovery_codes_used_after_created CHECK (used_at IS NULL OR used_at >= created_at)
);

CREATE INDEX recovery_codes_user_unused ON recovery_codes (tenant_id, user_id) WHERE used_at IS NULL;

ALTER TABLE totp_factors ENABLE ROW LEVEL SECURITY;
ALTER TABLE totp_factors FORCE ROW LEVEL SECURITY;
CREATE POLICY totp_factors_tenant ON totp_factors
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));

ALTER TABLE recovery_codes ENABLE ROW LEVEL SECURITY;
ALTER TABLE recovery_codes FORCE ROW LEVEL SECURITY;
CREATE POLICY recovery_codes_tenant ON recovery_codes
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
