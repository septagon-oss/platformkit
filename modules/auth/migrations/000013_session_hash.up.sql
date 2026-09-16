-- The session credential stops being a stored value.
--
-- Until now the cookie carried a uuid and that same uuid was the primary key:
-- anybody who could read the sessions table — a backup, a replica, a support
-- query, an SQL injection anywhere in the application — held every live
-- session, not a record of them. A session id is a password with a shorter
-- life, and a password is not stored.
--
-- So the row is keyed by sha256 of the id and the id itself is written nowhere.
-- The cookie carries the random value, the lookup hashes what it was given and
-- reads by key, and a dump is a list of hashes. SHA-256 without a work factor
-- is right here and would be wrong for a password: the input is 128 bits of
-- crypto/rand rather than something a person chose, so there is no dictionary
-- to run and nothing for a slow hash to buy.
--
-- Every session ends here, once. The old rows hold a plaintext credential and
-- no hash to migrate them to; keeping them would mean keeping the thing this
-- migration exists to remove, and signing everybody out is what changing the
-- shape of a credential costs.
DELETE FROM sessions;

ALTER TABLE sessions DROP COLUMN id;
ALTER TABLE sessions ADD COLUMN id_hash bytea NOT NULL;
ALTER TABLE sessions ADD PRIMARY KEY (id_hash);

-- The absolute cap: a session is refused and deleted thirty days after its last
-- use and ninety days after it was opened, whichever comes first. Without the
-- second, a sliding expiry is not an expiry at all — a browser that is used
-- every day keeps one session for as long as the laptop lasts, and a token
-- stolen from it works for as long as the thief keeps using it. The hourly
-- purge walks both, so this index is what makes the second one a range scan.
CREATE INDEX sessions_created_at ON sessions (created_at);
