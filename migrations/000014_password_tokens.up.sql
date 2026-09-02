-- The one-time token behind "set a password" and "I forgot mine".
--
-- It is the same row for both, because they are the same fact: somebody who
-- cannot sign in has been sent a link that lets them choose a password once.
-- An invitation and a reset differ in the message, not in the mechanism, and
-- two tables would be two expiries and two purges to keep in step.
--
-- Hashed, for the reason sessions are: this is a credential, and a credential
-- is not stored. SHA-256 without a work factor, because the input is 256 bits
-- of crypto/rand rather than something a person chose.
--
-- Sixty minutes, which is long enough for somebody to find the mail and short
-- enough that a link left in an inbox is not an account. Consuming it is a
-- DELETE ... RETURNING, so "used once" is the row being gone rather than a flag
-- two concurrent requests could both read as unset.
CREATE TABLE password_tokens (
	token_hash bytea PRIMARY KEY,
	tenant_id  uuid NOT NULL,
	user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	created_at timestamptz NOT NULL DEFAULT now(),
	expires_at timestamptz NOT NULL
);

-- One pending token per person: asking again replaces the last one rather than
-- adding to it, so a mailbox full of forgotten-password mails is still one live
-- link and the oldest of them stops working the moment a newer is asked for.
CREATE UNIQUE INDEX password_tokens_user ON password_tokens (tenant_id, user_id);

-- What the hourly purge walks.
CREATE INDEX password_tokens_expires_at ON password_tokens (expires_at);

ALTER TABLE password_tokens ENABLE ROW LEVEL SECURITY;
ALTER TABLE password_tokens FORCE ROW LEVEL SECURITY;

CREATE POLICY password_tokens_tenant ON password_tokens
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
