-- pkit: phase=data
-- pkit: batch=1000
-- pkit: table=users
WITH instant AS (SELECT clock_timestamp() AS at)
UPDATE users SET invitation_phase = 'pending', invitation_revision = 1,
    invitation_recipient = lower(trim(email)), invitation_adopted_at = instant.at,
    invitation_expires_at = instant.at + interval '1 hour'
FROM instant
WHERE users.id IN (SELECT id FROM batch) AND users.status = 'invited'
    AND users.deleted_at IS NULL AND users.invitation_phase IS NULL;
