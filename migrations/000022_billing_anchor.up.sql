-- The day of the month a subscription bills on.
--
-- It is stored rather than derived from the period being served, and that is
-- the whole of the fix. A monthly subscription bought on the 31st of January
-- has to be clamped to the 28th in February, and the next period was then
-- computed from the 28th: the 31st never came back, and every anniversary after
-- the first February was the 28th. See contracts.Advance.
--
-- Zero is "the day this period started", which is what every row written before
-- this column existed means; contracts.Advance reads it that way, so the
-- backfill below is a convenience and not a correctness requirement.
ALTER TABLE billing_subscriptions
	ADD COLUMN anchor_day integer NOT NULL DEFAULT 0;

ALTER TABLE billing_subscriptions
	ADD CONSTRAINT billing_subscriptions_anchor CHECK (anchor_day BETWEEN 0 AND 31);

-- The rows that already exist bill on the day their current period started,
-- which is the same answer the zero default gives and is cheaper to read.
UPDATE billing_subscriptions
	SET anchor_day = EXTRACT(DAY FROM current_period_start)::integer;
