-- pkit: autocommit=true

-- A channel is asked for many times and sent once.
--
-- The outbox redelivers a delivery it did not see acknowledged, and a provider
-- that answered after the attempt was retried would write a second terminal
-- row: coverage would still be satisfied, but "was she told twice" would have no
-- answer and the ledger's order would say the second mail was the first. The
-- database refuses the second `sent` rather than the worker remembering to
-- check. `suppressed` and `failed` stay repeatable on purpose — a channel that
-- failed, was retried and then worked must be able to say both — and `requested`
-- is written once by the notice itself.
--
-- The file is its own because the index is on a table another file created:
-- CONCURRENTLY is the only way to build it without a SHARE lock that stops every
-- writer on the ledger for the length of the build, and CONCURRENTLY cannot run
-- inside a transaction, which is the shape every other migration file has.
-- IF NOT EXISTS is what makes a re-run of a file that crashed half-built answer
-- "already done" rather than an error the installer has to reason about.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS notification_deliveries_sent_once
	ON notification_deliveries (notification_id, channel) WHERE outcome = 'sent';
