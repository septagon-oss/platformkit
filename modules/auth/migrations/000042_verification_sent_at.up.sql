-- A credential is offered in one transaction and mailed after that write commits,
-- so a process that stops in between leaves a row no recipient ever got. The
-- resend cooldown cannot key on created_at: that row would hold the cooldown down
-- and the retry would send nothing. sent_at records the delivery the transport
-- accepted, which is the only fact the cooldown and the retry both need.
ALTER TABLE verification_tokens ADD COLUMN sent_at timestamptz;
