-- The first half, marked as proved.
--
-- 000031 argues that this schema needs no table of challenges, and it is right
-- about the thing it argues: which codes are spent is RFC 6238's own state —
-- the step is the nonce, its window is the lifetime, and last_step is the one
-- integer that makes a replay impossible. That argument covers replay. The
-- question it leaves open is not "have I seen this code before" but "is this
-- caller entitled to be answering at all", and no step counter can answer a
-- question about the caller.
--
-- A module that answers that question with "anybody holding a code is" has
-- turned the code into the account, whichever code it is. A recovery code is
-- text a person keeps — a password manager, a file, a printed card in a drawer
-- — so a leaked set is ten sign-ins rather than ten ways into a second step,
-- and each spends once, which is what makes them ten and not one. A TOTP is
-- the same door with a 30-second timer on it, and six digits read out to
-- whoever phished them is the attack a second factor exists to be the answer
-- to. Neither of those needs a password at all under the previous shape of
-- this capability: the challenge leg took an address and a code and opened a
-- session, and nothing between the two asked whether a first half had ever
-- been offered.
--
-- So: one row per person whose first factor checked out and whose second half
-- is still outstanding. Minted by the refusal that says one more thing is
-- missing (Login and Open both answer ErrFactorRequired, and both mint), spent
-- by the challenge leg in the transaction that opens the session, and absent
-- for a caller who never offered the first proof — which is the whole of what
-- this table is for.
--
-- The mechanism is password_tokens', shape for shape, because that table has
-- already had the arguments had about it: one live row per person, so a person
-- refused four times has one window refreshed by the last refusal rather than
-- four; an expiry the hourly purge walks; and consumption by DELETE, so "used
-- once" is the row being gone rather than a flag two concurrent requests could
-- both read as unset.
--
-- It holds no token and no secret, and nothing like it is sent to the caller.
-- The row is a fact the server established by checking a password, not a
-- credential the caller presents, so there is no cookie, header or body field
-- that carries it, no value to phish, and a dumped copy of this table lets
-- nobody in: there is nothing in it to spend except the fact itself. That is
-- also why no hash sits here next to code_hash and token_hash.
--
-- Five minutes, in contracts.FirstFactorProofWindow rather than in a column
-- default, because the number is enforced by the statement that reads this
-- table and a deployment that widened it would have weakened itself: long
-- enough for a person on a shared machine to unlock a phone, find the
-- authenticator and type six digits, and no wider, because the only way to get
-- another window is to type the first factor again.
CREATE TABLE first_factor_proofs (
	-- The person, as the primary key: one outstanding window per account, which
	-- is the same one-live-credential-per-person rule password_tokens_user
	-- states with a unique index. A user row belongs to one tenant, so this key
	-- needs no tenant in it to be one per account.
	--
	-- No REFERENCES here, where every other credential table has one, and the
	-- reason is a lock rather than an oversight. A foreign key takes FOR KEY
	-- SHARE on the row it points at, and the federated leg holds that same
	-- person's row FOR UPDATE inside its own still-open transaction
	-- (users.ConfirmAddress → lockedUser) at the moment it refuses for the
	-- second half. Minting from the request's transaction is impossible — a 401
	-- rolls it back, which is why this write is detached at all — and a detached
	-- write with a foreign key here waits on a lock the request that summoned it
	-- is holding, which is a two-second stall on every federated sign-in and a
	-- marker that is never written. What the cascade would have bought is row
	-- removal, and the expiry does that better: the row is worthless after
	-- contracts.FirstFactorProofWindow and the hourly purge walks it, so no
	-- delete of a person can leave anything behind that outlives five minutes.
	user_id uuid PRIMARY KEY,
	tenant_id uuid NOT NULL,
	-- Proved_at is deliberately absent: the clock answers "is this window still
	-- open" and the purge reads expires_at, so a second timestamp would be a
	-- column nothing consults (rule 7).
	expires_at timestamptz NOT NULL
);

-- What the hourly purge walks, as password_tokens_expires_at does.
CREATE INDEX first_factor_proofs_expires_at ON first_factor_proofs (expires_at);

ALTER TABLE first_factor_proofs ENABLE ROW LEVEL SECURITY;
ALTER TABLE first_factor_proofs FORCE ROW LEVEL SECURITY;
CREATE POLICY first_factor_proofs_tenant ON first_factor_proofs
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
