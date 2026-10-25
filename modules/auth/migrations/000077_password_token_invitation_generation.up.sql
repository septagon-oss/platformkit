ALTER TABLE password_tokens
    ADD COLUMN invitation_generation bigint NOT NULL DEFAULT 0
    CHECK (invitation_generation >= 0);
