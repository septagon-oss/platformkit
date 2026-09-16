-- Independent mailbox activation must not accept a password-reset credential.
-- Auth owns this proof; user owns the exact unverified -> active transition.
-- Only the digest is stored. Neither events nor notification rows carry tokens.
CREATE TABLE verification_tokens (
	token_hash bytea PRIMARY KEY,
	tenant_id uuid NOT NULL,
	user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	email text NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	expires_at timestamptz NOT NULL,
	CONSTRAINT verification_tokens_user UNIQUE (tenant_id, user_id)
);

CREATE INDEX verification_tokens_expires_at ON verification_tokens (expires_at);
ALTER TABLE verification_tokens ENABLE ROW LEVEL SECURITY;
ALTER TABLE verification_tokens FORCE ROW LEVEL SECURITY;
CREATE POLICY verification_tokens_tenant ON verification_tokens
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));
