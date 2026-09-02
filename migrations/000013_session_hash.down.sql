-- Going back means going back to storing the credential, so the rows do not
-- come with it: there is no id to restore from a hash.
DELETE FROM sessions;
DROP INDEX IF EXISTS sessions_created_at;
ALTER TABLE sessions DROP COLUMN id_hash;
ALTER TABLE sessions ADD COLUMN id uuid PRIMARY KEY DEFAULT gen_random_uuid();
