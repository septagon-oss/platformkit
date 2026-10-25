SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '15s';

ALTER TABLE users
    ADD COLUMN invitation_phase text,
    ADD COLUMN invitation_revision bigint NOT NULL DEFAULT 0 CHECK (invitation_revision >= 0),
    ADD COLUMN invitation_generation bigint NOT NULL DEFAULT 0 CHECK (invitation_generation >= 0),
    ADD COLUMN invitation_recipient text,
    ADD COLUMN invitation_adopted_at timestamptz,
    ADD COLUMN invitation_requested_at timestamptz,
    ADD COLUMN invitation_expires_at timestamptz,
    ADD COLUMN invitation_delivery_id uuid,
    ADD COLUMN invitation_accepted_at timestamptz,
    ADD COLUMN invitation_accepted_via text,
    ADD COLUMN invitation_revoked_at timestamptz,
    ADD CONSTRAINT users_invitation_history CHECK (
        (invitation_phase IS NULL AND invitation_revision = 0 AND invitation_generation = 0
            AND invitation_recipient IS NULL AND invitation_adopted_at IS NULL
            AND invitation_requested_at IS NULL AND invitation_expires_at IS NULL
            AND invitation_delivery_id IS NULL AND invitation_accepted_at IS NULL
            AND invitation_accepted_via IS NULL AND invitation_revoked_at IS NULL) OR
        (invitation_phase IS NOT NULL AND invitation_phase IN ('pending', 'accepted', 'revoked')
            AND invitation_revision > 0 AND invitation_recipient IS NOT NULL
            AND invitation_recipient <> '' AND invitation_expires_at IS NOT NULL
            AND ((invitation_generation = 0 AND invitation_adopted_at IS NOT NULL
                    AND invitation_requested_at IS NULL AND invitation_delivery_id IS NULL)
                OR (invitation_generation > 0 AND invitation_requested_at IS NOT NULL
                    AND invitation_delivery_id IS NOT NULL))
            AND ((invitation_phase = 'pending' AND invitation_accepted_at IS NULL
                    AND invitation_accepted_via IS NULL AND invitation_revoked_at IS NULL)
                OR (invitation_phase = 'accepted' AND invitation_accepted_at IS NOT NULL
                    AND invitation_accepted_via IS NOT NULL AND invitation_accepted_via IN ('password_link', 'direct_password', 'identity_provider')
                    AND invitation_revoked_at IS NULL)
                OR (invitation_phase = 'revoked' AND invitation_revoked_at IS NOT NULL
                    AND invitation_accepted_at IS NULL AND invitation_accepted_via IS NULL)))
    );
